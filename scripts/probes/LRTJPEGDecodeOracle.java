package lrtlprobe;

import java.awt.image.BufferedImage;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Locale;
import javax.imageio.ImageIO;

/** Saves the installed LRTimelapse AWT decoder's raster for each JPEG input. */
public final class LRTJPEGDecodeOracle {
    public static void main(String[] args) throws Exception {
        if (args.length < 2) {
            throw new IllegalArgumentException("expected output JSON path and one or more JPEG paths");
        }
        Class<?> frameClass = Class.forName("tQa");
        Method decode = frameClass.getMethod("A", byte[].class);
        Class<?> sequenceClass = Class.forName("Rsa");
        Class<?> referenceClass = Class.forName("Nsa");
        Class<?> chartClass = Class.forName("Rqa");
        Class<?> propertiesClass = Class.forName("lrtimelapse.settings.SequenceProperties");
        Class<?> settingsClass = Class.forName("lrtimelapse.settings.LRTSettings");
        Object settings = settingsClass.getMethod("a").invoke(null);
        Field cacheField = frameClass.getDeclaredField("Ca");
        cacheField.setAccessible(true);
        Field sequenceProperties = sequenceClass.getDeclaredField("j");
        sequenceProperties.setAccessible(true);
        Field measurement = frameClass.getDeclaredField("Ua");
        measurement.setAccessible(true);
        Field edgeWeights = frameClass.getDeclaredField("Da");
        edgeWeights.setAccessible(true);
        Method measure = frameClass.getMethod("A", sequenceClass);
        Method histogram = frameClass.getMethod("A", chartClass, BufferedImage.class,
                double.class, double.class, double.class, double.class, boolean.class);

        StringBuilder json = new StringBuilder("[\n");
        for (int index = 1; index < args.length; index++) {
            String inputName = args[index];
            Path input = Path.of(inputName);
            byte[] encoded = Files.readAllBytes(input);
            BufferedImage decoded = (BufferedImage) decode.invoke(null, (Object) encoded);
            if (decoded == null) {
                throw new IllegalStateException("LRT could not decode " + input);
            }
            String name = input.getFileName().toString();
            int extension = name.lastIndexOf('.');
            if (extension >= 0) {
                name = name.substring(0, extension);
            }
            Path output = input.resolveSibling(name + "-lrt-decoded.png");
            if (!ImageIO.write(decoded, "png", output.toFile())) {
                throw new IllegalStateException("no PNG writer available for " + output);
            }

            Object frame = frameClass.getConstructor(String.class).newInstance(input.toString());
            Object reference = referenceClass.getConstructor(float.class, float.class,
                    float.class, float.class, boolean.class).newInstance(0f, 0f, 1f, 1f, false);
            frameClass.getMethod("A", referenceClass).invoke(frame, reference);
            Object sequence = sequenceClass.getConstructor().newInstance();
            ((java.util.Vector) sequence).add(frame);
            Object properties = propertiesClass.getConstructor().newInstance();
            propertiesClass.getField("deflickerIgnoreBlacksWhites").setInt(properties, 2);
            sequenceProperties.set(sequence, properties);
            settingsClass.getField("defaultDeflickerIgnoreBlacksWhites").setBoolean(settings, false);
            settingsClass.getField("defaultDeflickerIgnoreBlacks").setInt(settings, 0);
            settingsClass.getField("defaultDeflickerIgnoreWhites").setInt(settings, 0);
            settingsClass.getField("deflickerIgnoreBlacksWhitesHardCut").setBoolean(settings, false);

            Object cache = cacheField.get(frame);
            cache.getClass().getMethod("A", byte[].class).invoke(cache, (Object) encoded);
            edgeWeights.set(null, null);
            measure.invoke(frame, sequence);
            double directEV = ((Number) measurement.get(frame)).doubleValue();
            Object chart = chartClass.getConstructor().newInstance();
            double staticEV = ((Number) histogram.invoke(null, chart, decoded,
                    0d, 0d, 1d, 1d, false)).doubleValue() / 256d;
            if (Math.abs(directEV - staticEV) > 1e-12) {
                throw new IllegalStateException("LRT measurement paths disagree for " + input);
            }
            String decodedName = output.getFileName().toString();
            String fileName = input.getFileName().toString();
            json.append("  {\"file\":\"").append(fileName)
                    .append("\",\"decodedFile\":\"").append(decodedName)
                    .append("\",\"subsampling\":\"").append(subsampling(encoded)).append("\"")
                    .append(String.format(Locale.ROOT, ",\"directEV\":%.17g,\"staticEV\":%.17g}", directEV, staticEV))
                    .append(index + 1 == args.length ? "\n" : ",\n");
            System.out.printf("%s %dx%d directEV=%.12f -> %s%n", input,
                    decoded.getWidth(), decoded.getHeight(), directEV, output);
        }
        json.append("]\n");
        Files.writeString(Path.of(args[0]), json.toString(), StandardCharsets.UTF_8);
    }

    private static String subsampling(byte[] encoded) {
        int position = 2;
        while (position + 1 < encoded.length) {
            while (position < encoded.length && (encoded[position] & 0xff) != 0xff) {
                position++;
            }
            while (position < encoded.length && (encoded[position] & 0xff) == 0xff) {
                position++;
            }
            if (position >= encoded.length) {
                break;
            }
            int marker = encoded[position++] & 0xff;
            if (marker == 0xd8 || marker == 0xd9 || marker == 0x01 ||
                    marker >= 0xd0 && marker <= 0xd7) {
                continue;
            }
            if (position + 1 >= encoded.length) {
                break;
            }
            int length = (encoded[position] & 0xff) << 8 | encoded[position + 1] & 0xff;
            if (length < 2 || position + length > encoded.length) {
                break;
            }
            if (isStartOfFrame(marker)) {
                int payload = position + 2;
                int components = encoded[payload + 5] & 0xff;
                if (components < 3) {
                    return "grayscale";
                }
                int luma = encoded[payload + 7] & 0xff;
                int chroma = encoded[payload + 10] & 0xff;
                int horizontal = (luma >> 4) / (chroma >> 4);
                int vertical = (luma & 0x0f) / (chroma & 0x0f);
                if (horizontal == 1 && vertical == 1) return "4:4:4";
                if (horizontal == 2 && vertical == 1) return "4:2:2";
                if (horizontal == 2 && vertical == 2) return "4:2:0";
                if (horizontal == 1 && vertical == 2) return "4:4:0";
                if (horizontal == 4 && vertical == 1) return "4:1:1";
                if (horizontal == 4 && vertical == 2) return "4:1:0";
                return "unsupported-" + horizontal + "x" + vertical;
            }
            if (marker == 0xda) {
                break;
            }
            position += length;
        }
        return "unknown";
    }

    private static boolean isStartOfFrame(int marker) {
        return switch (marker) {
            case 0xc0, 0xc1, 0xc2, 0xc3, 0xc5, 0xc6, 0xc7, 0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf -> true;
            default -> false;
        };
    }
}
