package lrtlprobe;

import java.awt.RenderingHints;
import java.awt.image.BufferedImage;
import java.lang.reflect.Method;
import java.util.Locale;

/** Measures synthetic rasters before and after LRT 7.5.5's camera preview resize. */
public final class PreviewResizeOracle {
    private static final int PREVIEW_WIDTH = 1024;
    private static final int PREVIEW_HEIGHT = (int) (PREVIEW_WIDTH / 1.5f);

    public static void main(String[] args) throws Exception {
        Class<?> frameClass = Class.forName("tQa");
        Class<?> chartClass = Class.forName("Rqa");
        Object chart = chartClass.getConstructor().newInstance();
        Method resize = frameClass.getMethod("A", BufferedImage.class, int.class,
                int.class, Object.class, boolean.class);
        Method measure = frameClass.getMethod("A", chartClass, BufferedImage.class,
                double.class, double.class, double.class, double.class, boolean.class);

        int[][] dimensions = {{1300, 900}, {2048, 1024}, {1800, 2400}};
        System.out.print("[\n");
        for (int i = 0; i < dimensions.length; i++) {
            int width = dimensions[i][0];
            int height = dimensions[i][1];
            BufferedImage source = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
            for (int y = 0; y < height; y++) {
                for (int x = 0; x < width; x++) {
                    int r = (x * 37 + y * 11 + i * 17) & 255;
                    int g = (x * 13 + y * 41 + i * 29) & 255;
                    int b = (x * 23 + y * 7 + i * 43) & 255;
                    source.setRGB(x, y, (r << 16) | (g << 8) | b);
                }
            }
            BufferedImage preview = (BufferedImage) resize.invoke(null, source,
                    PREVIEW_WIDTH, PREVIEW_HEIGHT,
                    RenderingHints.VALUE_INTERPOLATION_BILINEAR, false);
            double previewEV = ((Number) measure.invoke(null, chart, preview,
                    0d, 0d, 1d, 1d, false)).doubleValue() / 256d;
            double sourceEV = ((Number) measure.invoke(null, chart, source,
                    0d, 0d, 1d, 1d, false)).doubleValue() / 256d;
            System.out.printf(Locale.ROOT,
                    "  {\"sourceWidth\":%d,\"sourceHeight\":%d,\"previewWidth\":%d,\"previewHeight\":%d,\"previewEV\":%.17g,\"sourceEV\":%.17g}%s%n",
                    width, height, preview.getWidth(), preview.getHeight(), previewEV,
                    sourceEV, i + 1 == dimensions.length ? "" : ",");
        }
        System.out.println("]");

        BufferedImage small = new BufferedImage(5, 3, BufferedImage.TYPE_INT_RGB);
        for (int y = 0; y < small.getHeight(); y++) {
            for (int x = 0; x < small.getWidth(); x++) {
                int r = (x * 47 + y * 19) & 255;
                int g = (x * 13 + y * 71) & 255;
                int b = (x * 83 + y * 31) & 255;
                small.setRGB(x, y, (r << 16) | (g << 8) | b);
            }
        }
        BufferedImage smallPreview = (BufferedImage) resize.invoke(null, small,
                3, 2, RenderingHints.VALUE_INTERPOLATION_BILINEAR, false);
        System.out.print("smallRGB=[");
        for (int y = 0; y < smallPreview.getHeight(); y++) {
            for (int x = 0; x < smallPreview.getWidth(); x++) {
                System.out.printf(Locale.ROOT, "%s%06x", x == 0 && y == 0 ? "" : ",",
                        smallPreview.getRGB(x, y) & 0xffffff);
            }
        }
        System.out.println("]");
    }
}
