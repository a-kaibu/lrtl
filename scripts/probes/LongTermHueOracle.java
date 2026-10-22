package lrtlprobe;

import java.awt.Color;
import java.nio.file.Files;
import java.nio.file.Path;
import java.security.MessageDigest;
import java.util.HexFormat;

// Public Java API measurements; each digest covers one fixed red component.
public final class LongTermHueOracle {
    public static void main(String[] args) throws Exception {
        StringBuilder out = new StringBuilder("[\n");
        float[] hsb = new float[3];
        byte[] block = new byte[256 * 256 * 4];
        for (int red = 0; red < 256; red++) {
            int cursor = 0;
            for (int green = 0; green < 256; green++) for (int blue = 0; blue < 256; blue++) {
                Color.RGBtoHSB(red, green, blue, hsb);
                int bits = Float.floatToRawIntBits(hsb[0]);
                for (int shift = 0; shift < 32; shift += 8) block[cursor++] = (byte)(bits >>> shift);
            }
            if (red != 0) out.append(",\n");
            out.append('"').append(HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(block))).append('"');
        }
        Files.writeString(Path.of(args[0]), out.append("\n]\n"));
    }
}
