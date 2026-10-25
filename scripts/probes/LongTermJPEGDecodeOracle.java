package lrtlprobe;

import java.awt.image.BufferedImage;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.Comparator;
import java.util.stream.Stream;

// Measures encoded JPEG inputs after decoding them through the same tQa byte
// decoder used by LRT's cached-frame path, then calls the native metric helper.
// This isolates file decoding plus long-term metrics without opening the GUI.
public final class LongTermJPEGDecodeOracle {
    static Method method(Class<?> type, String name, Class<?> result, Class<?>... parameters) {
        Method found = Arrays.stream(type.getDeclaredMethods()).filter(m ->
            m.getName().equals(name) && m.getReturnType() == result &&
            Arrays.equals(m.getParameterTypes(), parameters)).findFirst().orElseThrow();
        found.setAccessible(true);
        return found;
    }

    static String quote(String value) {
        return "\"" + value.replace("\\", "\\\\").replace("\"", "\\\"") + "\"";
    }

    public static void main(String[] args) throws Exception {
        Class<?> frame = Class.forName("tQa");
        Class<?> analyzer = Class.forName("ORa");
        Class<?> result = Class.forName("xQa");
        Method decode = frame.getMethod("A", byte[].class);
        Method analyze = method(analyzer, "A", result, BufferedImage.class,
            double.class, double.class, double.class, double.class, boolean.class);

        StringBuilder out = new StringBuilder("{\"version\":1,\"cases\":[\n");
        int count = 0;
        try (Stream<Path> paths = Files.list(Path.of(args[0]))) {
            for (Path path : paths.filter(p -> {
                    String name = p.getFileName().toString().toLowerCase();
                    return name.startsWith("rgb-") && name.endsWith(".jpg");
                })
                    .sorted(Comparator.comparing(p -> p.getFileName().toString())).toList()) {
                byte[] bytes = Files.readAllBytes(path);
                BufferedImage image = (BufferedImage) decode.invoke(null, (Object) bytes);
                if (image == null) throw new IllegalStateException("LRT decoder returned null for " + path);
                Object measured = analyze.invoke(null, image, 0., 0., 1., 1., false);
                if (count++ > 0) out.append(",\n");
                out.append("{\"file\":").append(quote(path.getFileName().toString()))
                    .append(",\"width\":").append(image.getWidth())
                    .append(",\"height\":").append(image.getHeight())
                    .append(",\"luminance\":").append(result.getField("l").getDouble(measured) / 256.)
                    .append(",\"contrast\":").append(result.getField("m").getDouble(measured) * 2.)
                    .append(",\"hue\":").append(result.getField("I").getDouble(measured) * 360.)
                    .append('}');
            }
        }
        if (count == 0) throw new IllegalArgumentException("no JPEG files in " + args[0]);
        Files.writeString(Path.of(args[1]), out.append("\n]}\n"));
        System.out.println("Observed LRT decoded JPEG long-term metrics: " + count);
    }
}
