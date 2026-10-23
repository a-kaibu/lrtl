import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.text.DecimalFormat;
import java.text.DecimalFormatSymbols;
import java.util.Locale;

// Original observer of the public JDK formatter used for internal exposures.
// This probe does not load LRTimelapse or any application state.
public final class InternalExposureFormatOracle {
    public static void main(String[] args) throws Exception {
        var format = new DecimalFormat("0.########", DecimalFormatSymbols.getInstance(Locale.US));
        var out = new StringBuilder();
        for (int exponent = 0; exponent < 2047; exponent += 31) {
            for (long mantissa : new long[]{0, 1, (1L << 52)-1}) {
                long bits = ((long)exponent << 52) | mantissa;
                append(out, format, bits);
                append(out, format, bits | Long.MIN_VALUE);
            }
        }
        long bits = 0x123456789abcdefL;
        for (int i=0; i<1024; i++) {
            bits ^= bits << 13; bits ^= bits >>> 7; bits ^= bits << 17;
            append(out, format, bits);
        }
        Files.writeString(Path.of(args[0]), out.toString(), StandardCharsets.UTF_8);
    }
    static void append(StringBuilder out, DecimalFormat format, long bits) {
        out.append(Long.toUnsignedString(bits,16)).append('\t');
        out.append(format.format(Double.longBitsToDouble(bits))).append('\n');
    }
}
