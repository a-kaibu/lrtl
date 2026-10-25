package lrtlprobe;

import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Locale;
import javax.imageio.ImageIO;

/** Calls LRT 7.5.5's cached-frame Deflicker measurement on synthetic JPEGs. */
public final class NativeDeflickerImageOracle {
    private record Case(int width, int height, int pattern, float x1, float y1,
            float x2, float y2, boolean ignore, int blacks, int whites,
            boolean hardCut) {}

    private static final Case[] CASES = {
        new Case(37, 23, 0, 0, 0, 1, 1, false, 0, 0, false),
        new Case(37, 23, 1, .17f, .13f, .83f, .91f, false, 0, 0, false),
        new Case(53, 31, 2, .13f, .2f, .9f, .8f, true, 10, 10, false),
        new Case(53, 31, 3, .1f, .1f, .95f, .95f, true, 15, 5, true),
        new Case(33, 17, 4, .25f, 0, .75f, 1, true, 5, 20, false),
        new Case(37, 23, 5, .33f, .25f, .7f, .95f, false, 25, 25, true),
    };

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("expected output directory and JSON path");
        }
        Path output = Path.of(args[0]);
        Files.createDirectories(output);

        Class<?> frameClass = Class.forName("tQa");
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

        StringBuilder json = new StringBuilder("[\n");
        for (int index = 0; index < CASES.length; index++) {
            Case tc = CASES[index];
            BufferedImage image = pattern(tc.width(), tc.height(), tc.pattern());
            ByteArrayOutputStream encoded = new ByteArrayOutputStream();
            ImageIO.write(image, "jpg", encoded);
            String fileName = String.format(Locale.ROOT, "native-deflicker-measure-%02d.jpg", index);
            Files.write(output.resolve(fileName), encoded.toByteArray());

            Object frame = frameClass.getConstructor(String.class).newInstance(fileName);
            Object reference = referenceClass.getConstructor(float.class, float.class,
                    float.class, float.class, boolean.class)
                    .newInstance(tc.x1(), tc.y1(), tc.x2(), tc.y2(), false);
            frameClass.getMethod("A", referenceClass).invoke(frame, reference);

            Object sequence = sequenceClass.getConstructor().newInstance();
            ((java.util.Vector) sequence).add(frame);
            Object properties = propertiesClass.getConstructor().newInstance();
            propertiesClass.getField("deflickerIgnoreBlacksWhites")
                    .setInt(properties, tc.ignore() ? 1 : 2);
            sequenceProperties.set(sequence, properties);

            settingsClass.getField("defaultDeflickerIgnoreBlacksWhites").setBoolean(settings, tc.ignore());
            settingsClass.getField("defaultDeflickerIgnoreBlacks").setInt(settings, tc.blacks());
            settingsClass.getField("defaultDeflickerIgnoreWhites").setInt(settings, tc.whites());
            settingsClass.getField("deflickerIgnoreBlacksWhitesHardCut").setBoolean(settings, tc.hardCut());

            Object cache = cacheField.get(frame);
            cache.getClass().getMethod("A", byte[].class).invoke(cache, (Object) encoded.toByteArray());
            edgeWeights.set(null, null);
            Method measure = frameClass.getMethod("A", sequenceClass);
            measure.invoke(frame, sequence);
            double previewEV = ((Number) measurement.get(frame)).doubleValue();
            BufferedImage decoded = (BufferedImage) frameClass.getMethod("A", byte[].class)
                    .invoke(null, (Object) encoded.toByteArray());
            frameClass.getMethod("A", BufferedImage.class, sequenceClass)
                    .invoke(frame, decoded, sequence);
            double directEV = ((Number) measurement.get(frame)).doubleValue();
            Object chart = chartClass.getConstructor().newInstance();
            Method histogram = frameClass.getMethod("A", chartClass, BufferedImage.class,
                    double.class, double.class, double.class, double.class, boolean.class);
            double staticEV = ((Number) histogram.invoke(null, chart, decoded, tc.x1(), tc.y1(),
                    tc.x2(), tc.y2(), tc.ignore())).doubleValue() / 256d;
            String decodedName = fileName.replace(".jpg", "-lrt-decoded.png");
            ImageIO.write(decoded, "png", output.resolve(decodedName).toFile());

            json.append("  {\"file\":\"").append(fileName)
                    .append("\",\"decodedFile\":\"").append(decodedName)
                    .append("\",\"width\":").append(tc.width())
                    .append(",\"height\":").append(tc.height())
                    .append(",\"pattern\":").append(tc.pattern())
                    .append(",\"roi\":[").append(tc.x1()).append(',').append(tc.y1()).append(',')
                    .append(tc.x2()).append(',').append(tc.y2()).append(']')
                    .append(",\"ignore\":").append(tc.ignore())
                    .append(",\"ignoreBlacks\":").append(tc.blacks())
                    .append(",\"ignoreWhites\":").append(tc.whites())
                    .append(",\"hardCut\":").append(tc.hardCut())
                    .append(String.format(Locale.ROOT, ",\"directEV\":%.17g", directEV))
                    .append(String.format(Locale.ROOT, ",\"staticEV\":%.17g", staticEV))
                    .append(String.format(Locale.ROOT, ",\"previewEV\":%.17g}", previewEV))
                    .append(index + 1 == CASES.length ? "\n" : ",\n");
        }
        json.append("]\n");
        Files.writeString(Path.of(args[1]), json.toString(), StandardCharsets.UTF_8);
    }

    private static BufferedImage pattern(int width, int height, int pattern) {
        BufferedImage image = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
        for (int y = 0; y < height; y++) {
            for (int x = 0; x < width; x++) {
                int red = (x * 47 + y * 19 + pattern * 17) & 255;
                int green = (x * 13 + y * 71 + pattern * 29) & 255;
                int blue = (x * 83 + y * 31 + pattern * 43) & 255;
                image.setRGB(x, y, red << 16 | green << 8 | blue);
            }
        }
        return image;
    }
}
