package lrtlprobe;

import java.awt.RenderingHints;
import java.awt.image.BufferedImage;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;
import java.util.Base64;
import java.io.ByteArrayOutputStream;
import java.util.zip.DeflaterOutputStream;

// Original synthetic measurement harness. Application implementations are not
// distributed; the installed JAR is used only to collect reference results.
public final class LongTermMetricOracle {
    static Method method(Class<?> type, String name, Class<?> result, Class<?>... parameters) {
        Method found = Arrays.stream(type.getDeclaredMethods()).filter(m ->
            m.getName().equals(name) && m.getReturnType() == result &&
            Arrays.equals(m.getParameterTypes(), parameters)).findFirst().orElseThrow();
        found.setAccessible(true);
        return found;
    }

    static String pixels(BufferedImage image) throws Exception {
        byte[] bytes = new byte[image.getWidth() * image.getHeight() * 4];
        int i = 0;
        for (int y = 0; y < image.getHeight(); y++) for (int x = 0; x < image.getWidth(); x++) {
            int rgb = image.getRGB(x, y);
            bytes[i++] = (byte)(rgb >> 16);
            bytes[i++] = (byte)(rgb >> 8);
            bytes[i++] = (byte)rgb;
            bytes[i++] = (byte)(rgb >> 24);
        }
        ByteArrayOutputStream compressed = new ByteArrayOutputStream();
        try (DeflaterOutputStream stream = new DeflaterOutputStream(compressed)) { stream.write(bytes); }
        return Base64.getEncoder().encodeToString(compressed.toByteArray());
    }

    public static void main(String[] args) throws Exception {
        Class<?> frame = Class.forName("tQa"), analyzer = Class.forName("ORa");
        Class<?> result = Class.forName("xQa"), gaussian = Class.forName("qra");
        Method resize = method(frame, "A", BufferedImage.class, BufferedImage.class,
            int.class, int.class, Object.class, boolean.class);
        Method analyze = method(analyzer, "A", result, BufferedImage.class,
            double.class, double.class, double.class, double.class, boolean.class);
        Object blur = gaussian.getConstructor(float.class).newInstance(4f);
        Method filter = gaussian.getMethod("filter", BufferedImage.class, BufferedImage.class);
        StringBuilder out = new StringBuilder("[\n");
        int count = 0;
        for (boolean alpha : new boolean[]{false,true})
        for (int[] size : new int[][]{{1,1},{2,3},{17,11},{64,64},{65,63},{127,91},{257,193}})
        for (int pattern = 0; pattern < 8; pattern++) {
            int width = size[0], height = size[1];
            BufferedImage image = new BufferedImage(width, height, alpha ? BufferedImage.TYPE_INT_ARGB : BufferedImage.TYPE_INT_RGB);
            for (int y = 0; y < height; y++) for (int x = 0; x < width; x++) {
                int r, g, b;
                switch (pattern) {
                    case 0: r = g = b = 255; break;
                    case 1: r = 255; g = b = 0; break;
                    case 2: r = g = b = (x * 255 / Math.max(1, width - 1)); break;
                    case 3: r = g = b = ((x + y) % 2) * 255; break;
                    case 4: r = (x*31+y*17+13)&255; g = (x*7+y*43+61)&255; b = (x*53+y*3+101)&255; break;
                    case 5: r = x < width/2 ? 255 : 0; g = y < height/2 ? 255 : 0; b = 64; break;
                    case 6: r = g = b = 0; if(x==width/2 && y==height/2) r = g = b = 255; break;
                    default: r = g = b = 0; break;
                }
                int a = alpha ? (x*19+y*29+pattern*37)&255 : 255;
                image.setRGB(x,y,(a<<24)|(r<<16)|(g<<8)|b);
            }
            Object measured = analyze.invoke(null,image,0.,0.,1.,1.,false);
            BufferedImage sampled = (BufferedImage)resize.invoke(null,image,64,64,RenderingHints.VALUE_INTERPOLATION_BILINEAR,false);
            BufferedImage blurred = (BufferedImage)filter.invoke(blur,sampled,null);
            if(count++ > 0) out.append(",\n");
            out.append("{\"Width\":").append(width).append(",\"Height\":").append(height)
                .append(",\"Pattern\":").append(pattern)
                .append(",\"Alpha\":").append(alpha)
                .append(",\"Luminance\":").append(result.getField("l").getDouble(measured)/256.)
                .append(",\"Contrast\":").append(result.getField("m").getDouble(measured)*2.)
                .append(",\"Hue\":").append(result.getField("I").getDouble(measured)*360.)
                .append(",\"Sampled\":\"").append(pixels(sampled))
                .append("\",\"Blurred\":\"").append(pixels(blurred)).append("\"}");
        }
        Files.writeString(Path.of(args[0]),out.append("\n]\n"));
        System.out.println("Observed long-term cases: " + count);
    }
}
