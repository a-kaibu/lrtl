package lrtlprobe;

import java.awt.RenderingHints;
import java.awt.image.BufferedImage;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Locale;

/** Measures long-term metrics after LRT 7.5.5 camera-preview width scaling. */
public final class LongTermPreviewMetricOracle {
    private static final int[][] CASES = {
        {1300, 900, 1024, 4},
        {2048, 1024, 1024, 1},
        {1800, 2400, 1024, 5},
        {1024, 1600, 1024, 4},
        {1300, 900, 512, 4},
    };

    public static void main(String[] args) throws Exception {
        if (args.length != 1) {
            throw new IllegalArgumentException("expected output JSON path");
        }
        Class<?> frame = Class.forName("tQa");
        Class<?> analyzer = Class.forName("ORa");
        Class<?> result = Class.forName("xQa");
        Method resize = frame.getMethod("A", BufferedImage.class, int.class,
                int.class, Object.class, boolean.class);
        Method measure = analyzer.getMethod("A", BufferedImage.class, double.class,
                double.class, double.class, double.class, boolean.class);
        StringBuilder json = new StringBuilder("[\n");
        for (int index = 0; index < CASES.length; index++) {
            int[] test = CASES[index];
            int width = test[0], height = test[1], limit = test[2], pattern = test[3];
            BufferedImage source = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
            for (int y = 0; y < height; y++) {
                for (int x = 0; x < width; x++) {
                    int r = (x * 37 + y * 11 + pattern * 17) & 255;
                    int g = (x * 13 + y * 41 + pattern * 29) & 255;
                    int b = (x * 23 + y * 7 + pattern * 43) & 255;
                    if (pattern == 1) {
                        r = 255;
                        g = b = 0;
                    } else if (pattern == 4) {
                        r = (x * 31 + y * 17 + 13) & 255;
                        g = (x * 7 + y * 43 + 61) & 255;
                        b = (x * 53 + y * 3 + 101) & 255;
                    } else if (pattern == 5) {
                        r = x < width / 2 ? 255 : 0;
                        g = y < height / 2 ? 255 : 0;
                        b = 64;
                    }
                    source.setRGB(x, y, (r << 16) | (g << 8) | b);
                }
            }
            int previewHeight = (int) (limit / 1.5f);
            BufferedImage preview = source;
            if (source.getWidth() > limit) {
                preview = (BufferedImage) resize.invoke(null, source, limit, previewHeight,
                        RenderingHints.VALUE_INTERPOLATION_BILINEAR, false);
            }
            Object measured = measure.invoke(null, preview, 0d, 0d, 1d, 1d, false);
            if (index > 0) json.append(",\n");
            json.append("  {\"sourceWidth\":").append(width)
                    .append(",\"sourceHeight\":").append(height)
                    .append(",\"previewWidthCamera\":").append(limit)
                    .append(",\"pattern\":").append(pattern)
                    .append(",\"width\":").append(preview.getWidth())
                    .append(",\"height\":").append(preview.getHeight())
                    .append(",\"luminance\":").append(String.format(Locale.ROOT, "%.17g", result.getField("l").getDouble(measured) / 256d))
                    .append(",\"contrast\":").append(String.format(Locale.ROOT, "%.17g", result.getField("m").getDouble(measured) * 2d))
                    .append(",\"hue\":").append(String.format(Locale.ROOT, "%.17g", result.getField("I").getDouble(measured) * 360d))
                    .append('}');
        }
        Files.writeString(Path.of(args[0]), json.append("\n]\n"));
    }
}
