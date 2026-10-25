package lrtlprobe;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardOpenOption;
import java.util.Locale;
import java.util.zip.ZipEntry;
import java.util.zip.ZipInputStream;
import java.util.zip.ZipOutputStream;

/** Creates a temporary unsigned copy for isolated reflection probes; never edits the input JAR. */
public final class UnsignedJarCopy {
    public static void main(String[] args) throws IOException {
        if (args.length != 2) {
            throw new IllegalArgumentException("expected input JAR and new output JAR paths");
        }
        Path input = Path.of(args[0]);
        Path output = Path.of(args[1]);
        if (input.toAbsolutePath().normalize().equals(output.toAbsolutePath().normalize())) {
            throw new IllegalArgumentException("input and output JAR must be different files");
        }

        try (ZipInputStream source = new ZipInputStream(Files.newInputStream(input));
                ZipOutputStream target = new ZipOutputStream(Files.newOutputStream(output, StandardOpenOption.CREATE_NEW))) {
            for (ZipEntry entry; (entry = source.getNextEntry()) != null; source.closeEntry()) {
                if (isSignatureBlock(entry.getName())) continue;
                ZipEntry copy = new ZipEntry(entry.getName());
                if (entry.getTime() >= 0) copy.setTime(entry.getTime());
                target.putNextEntry(copy);
                source.transferTo(target);
                target.closeEntry();
            }
        }
    }

    private static boolean isSignatureBlock(String name) {
        String upper = name.toUpperCase(Locale.ROOT);
        if (!upper.startsWith("META-INF/")) return false;
        String localName = upper.substring("META-INF/".length());
        if (localName.indexOf('/') >= 0) return false;
        return localName.endsWith(".SF") || localName.endsWith(".RSA")
                || localName.endsWith(".DSA") || localName.endsWith(".EC")
                || localName.endsWith(".SIG");
    }
}
