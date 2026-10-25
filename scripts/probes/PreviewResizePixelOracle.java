package lrtlprobe;

import java.awt.RenderingHints;
import java.awt.image.BufferedImage;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Locale;

/** Writes full-pixel resize references from the installed LRT 7.5.5 helper. */
public final class PreviewResizePixelOracle {
    private static final int[][] CASES = {
        {5, 3, 3, 2},
        {2, 2, 4, 4},
        {4, 3, 2, 2},
        {7, 5, 4, 3},
        {9, 7, 5, 4},
        {7, 5, 6, 4},
    };

    public static void main(String[] args) throws Exception {
        if (args.length != 1) {
            throw new IllegalArgumentException("expected output JSON path");
        }
        Class<?> frameClass = Class.forName("tQa");
        Method resize = frameClass.getMethod("A", BufferedImage.class, int.class,
                int.class, Object.class, boolean.class);
        StringBuilder json = new StringBuilder("[\n");
        for (int index = 0; index < CASES.length; index++) {
            int[] dimensions = CASES[index];
            BufferedImage source = new BufferedImage(dimensions[0], dimensions[1],
                    BufferedImage.TYPE_INT_RGB);
            for (int y = 0; y < source.getHeight(); y++) {
                for (int x = 0; x < source.getWidth(); x++) {
                    int r = (x * 47 + y * 19 + index * 17) & 255;
                    int g = (x * 13 + y * 71 + index * 29) & 255;
                    int b = (x * 83 + y * 31 + index * 43) & 255;
                    source.setRGB(x, y, (r << 16) | (g << 8) | b);
                }
            }
            BufferedImage resized = (BufferedImage) resize.invoke(null, source,
                    dimensions[2], dimensions[3],
                    RenderingHints.VALUE_INTERPOLATION_BILINEAR, false);
            json.append("  {\"sourceWidth\":").append(dimensions[0])
                    .append(",\"sourceHeight\":").append(dimensions[1])
                    .append(",\"width\":").append(dimensions[2])
                    .append(",\"height\":").append(dimensions[3])
                    .append(",\"pattern\":").append(index)
                    .append(",\"rgb\":[");
            for (int y = 0; y < resized.getHeight(); y++) {
                for (int x = 0; x < resized.getWidth(); x++) {
                    if (x != 0 || y != 0) {
                        json.append(',');
                    }
                    json.append('"').append(String.format(Locale.ROOT, "%06x",
                            resized.getRGB(x, y) & 0xffffff)).append('"');
                }
            }
            json.append(index + 1 == CASES.length ? "]}\n" : "]},\n");
        }
        json.append("]\n");
        Files.writeString(Path.of(args[0]), json.toString(), StandardCharsets.UTF_8);
    }
}
