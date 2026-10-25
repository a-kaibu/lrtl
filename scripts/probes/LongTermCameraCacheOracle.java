package lrtlprobe;

import java.awt.image.BufferedImage;
import java.io.File;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.security.MessageDigest;
import java.util.Arrays;
import java.util.HexFormat;
import java.util.Locale;

/** Records which cached image the LRT 7.5.5 native frame worker measures. */
public final class LongTermCameraCacheOracle {
    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            throw new IllegalArgumentException("expected cache-JPEG output and metric JSON output paths");
        }

        Path sequence = Files.createTempDirectory("lrt-longterm-camera-cache-");
        Class<?> main = Class.forName("lrtimelapse.LRTimelapseMain");
        Class<?> frameClass = Class.forName("tQa");

        File source = sequence.resolve("frame.jpg").toFile();
        writeJpeg(frameClass, solid(192, 96, 0xff0000), source.toPath());

        File cameraCache = (File) main.getMethod("h", File.class).invoke(null, source);
        Files.createDirectories(cameraCache.toPath().getParent());
        writeJpeg(frameClass, solid(192, 96, 0x0000ff), cameraCache.toPath());
        Files.copy(cameraCache.toPath(), Path.of(args[0]), StandardCopyOption.REPLACE_EXISTING);

        File visualCache = (File) main.getMethod("j", File.class).invoke(null, source);
        Files.createDirectories(visualCache.toPath().getParent());
        writeJpeg(frameClass, solid(192, 96, 0x00ff00), visualCache.toPath());

        Object frame = frameClass.getConstructor(String.class).newInstance(source.getPath());
        frameClass.getMethod("a", File.class).invoke(frame, source);
        frameClass.getMethod("A", String.class).invoke(frame, source.getName());
        Object sequenceState = Class.forName("Rsa").getConstructor().newInstance();
        Class<?> settingsClass = Class.forName("lrtimelapse.settings.LRTSettings");
        Object settings = settingsClass.getMethod("a").invoke(null);
        int previewWidth = settingsClass.getField("previewWidthCamera").getInt(settings);
        int previewHeight = (int) (previewWidth / 1.5f);
        Method loadFrame = frameClass.getMethod("A",
                Class.forName("lrtimelapse.tools.Prn"), sequenceState.getClass(),
                int.class, int.class, boolean.class, int.class, boolean.class);
        // false allows the native worker to read an existing .lrt/previews cache.
        loadFrame.invoke(frame, null, sequenceState, previewWidth, previewHeight, false, -1, false);

        Method getLoadedImage = Arrays.stream(frameClass.getMethods())
                .filter(method -> method.getName().equals("K") && method.getParameterCount() == 0
                        && method.getReturnType() == BufferedImage.class)
                .findFirst().orElseThrow();
        BufferedImage loaded = (BufferedImage) getLoadedImage.invoke(frame);
        if (loaded == null) throw new IllegalStateException("LRT frame worker did not produce an image");

        Class<?> analyzer = Class.forName("ORa");
        Method measure = analyzer.getMethod("A", BufferedImage.class,
                double.class, double.class, double.class, double.class, boolean.class);
        Object metrics = measure.invoke(null, loaded, 0d, 0d, 1d, 1d, false);
        Class<?> metricClass = metrics.getClass();
        double luminance = metricClass.getField("l").getDouble(metrics) / 256d;
        double contrast = metricClass.getField("m").getDouble(metrics) * 2d;
        double hue = metricClass.getField("I").getDouble(metrics) * 360d;

        String json = "{\n"
                + "  \"source\": \"solid red 192x96 JPEG\",\n"
                + "  \"cameraCache\": \"solid blue 192x96 JPEG\",\n"
                + "  \"visualCache\": \"solid green 192x96 JPEG\",\n"
                + "  \"selectedCache\": \"camera\",\n"
                + "  \"width\": " + loaded.getWidth() + ",\n"
                + "  \"height\": " + loaded.getHeight() + ",\n"
                + "  \"rgbSha256\": \"" + rgbSha256(loaded) + "\",\n"
                + "  \"luminance\": " + String.format(Locale.ROOT, "%.17g", luminance) + ",\n"
                + "  \"contrast\": " + String.format(Locale.ROOT, "%.17g", contrast) + ",\n"
                + "  \"hue\": " + String.format(Locale.ROOT, "%.17g", hue) + "\n"
                + "}\n";
        Files.writeString(Path.of(args[1]), json);
        System.out.print(json);
    }

    private static BufferedImage solid(int width, int height, int rgb) {
        BufferedImage image = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
        for (int y = 0; y < height; y++) {
            for (int x = 0; x < width; x++) image.setRGB(x, y, rgb);
        }
        return image;
    }

    private static void writeJpeg(Class<?> frameClass, BufferedImage image, Path target) throws Exception {
        Method encode = Arrays.stream(frameClass.getMethods())
                .filter(method -> method.getName().equals("A")
                        && Arrays.equals(method.getParameterTypes(), new Class<?>[] {BufferedImage.class})
                        && method.getReturnType() == byte[].class)
                .findFirst().orElseThrow();
        byte[] bytes = (byte[]) encode.invoke(null, image);
        Files.write(target, bytes);
    }

    private static String rgbSha256(BufferedImage image) throws Exception {
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        for (int y = 0; y < image.getHeight(); y++) {
            for (int x = 0; x < image.getWidth(); x++) {
                int rgb = image.getRGB(x, y);
                digest.update((byte) (rgb >>> 16));
                digest.update((byte) (rgb >>> 8));
                digest.update((byte) rgb);
            }
        }
        return HexFormat.of().formatHex(digest.digest());
    }
}
