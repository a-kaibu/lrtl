import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Enumeration;
import java.util.List;
import java.util.Locale;
import java.util.jar.JarEntry;
import java.util.jar.JarFile;
import java.util.jar.JarOutputStream;

/** Captures LRTimelapse 7.5.5's installed keyframe quantity helper for synthetic sequences. */
@SuppressWarnings("removal")
public final class LRTKeyframeQuantityOracle {
    private record Case(String name, double[] measurements, int[] ratings) {}

    public static void main(String[] args) throws Exception {
        if (args.length < 1 || args.length > 2) {
            throw new IllegalArgumentException("expected output JSON path and optional installed LRTimelapse JAR path");
        }

        Path sourceJar = Path.of(args.length == 2 ? args[1] : "C:/Program Files/LRTimelapse 7/LRTimelapse.jar");
        Path unsignedJar = Path.of(System.getProperty("java.io.tmpdir"), "lrtimelapse-7.5.5-oracle-unsigned.jar");
        copyWithoutSignatures(sourceJar, unsignedJar);
        ClassLoader lrtLoader = new java.net.URLClassLoader(
                new java.net.URL[]{unsignedJar.toUri().toURL()}, LRTKeyframeQuantityOracle.class.getClassLoader());

        Class<?> controllerClass = Class.forName("lrtimelapse.cOM7", true, lrtLoader);
        Class<?> tableModelClass = Class.forName("lrtimelapse.com8", true, lrtLoader);
        Class<?> sequenceClass = Class.forName("Rsa", true, lrtLoader);
        Class<?> frameClass = Class.forName("tQa", true, lrtLoader);

        Class<?> unsafeClass = Class.forName("sun.misc.Unsafe");
        Field unsafeField = unsafeClass.getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        Object unsafe = unsafeField.get(null);

        Object controller = unsafeClass.getMethod("allocateInstance", Class.class).invoke(unsafe, controllerClass);
        Object tableModel = tableModelClass
                .getConstructor(ArrayList.class)
                .newInstance(new ArrayList<String>());
        Field tableField = controllerClass.getDeclaredField("wc");
        tableField.setAccessible(true);
        tableField.set(controller, tableModel);
        Method getSequence = Arrays.stream(tableModelClass.getMethods())
                .filter(method -> method.getName().equals("A") && method.getReturnType() == sequenceClass)
                .findFirst().orElseThrow();
        Object sequence = getSequence.invoke(tableModel);

        Constructor<?> frameConstructor = frameClass.getConstructor(String.class);
        Method setMeasurement = frameClass.getMethod("M", Double.class);
        Method setRating = frameClass.getMethod("E", int.class);
        Method autoCount = controllerClass.getDeclaredMethod("A", int.class, int.class);
        autoCount.setAccessible(true);

        List<Case> cases = List.of(
                new Case("flat_two_frames", constant(2, 0), ratings(2)),
                new Case("range_018_150_frames_spacing_limit", linear(150, 0.18), ratings(150)),
                new Case("count_four_spacing_boundary_251", linear(251, rangeForCount(251, 4.999)), ratings(251)),
                new Case("count_five_spacing_boundary_251", linear(251, rangeForCount(251, 5.5)), ratings(251)),
                new Case("minimum_anchor_floor_1001", constant(1001, 0), ratings(1001)),
                new Case("holy_grail_pair_compensates_jump_500", step(500, 250, 2), pairRatings(500, 249, 250)),
                new Case("single_holy_grail_marker_keeps_range_500", step(500, 250, 2), singleRating(500, 250, 3)),
                new Case("spacing_cap_251", linear(251, 20), ratings(251))
        );

        StringBuilder json = new StringBuilder("{\n  \"source\": \"LRTimelapse 7.5.5 build 1059 installed runtime\",\n  \"cases\": [\n");
        for (int caseIndex = 0; caseIndex < cases.size(); caseIndex++) {
            Case test = cases.get(caseIndex);
            sequenceClass.getMethod("clear").invoke(sequence);
            for (int index = 0; index < test.measurements().length; index++) {
                Object frame = frameConstructor.newInstance(String.format(Locale.ROOT, "oracle-%05d.jpg", index));
                setMeasurement.invoke(frame, test.measurements()[index]);
                if (test.ratings()[index] != 0) {
                    setRating.invoke(frame, test.ratings()[index]);
                }
                sequenceClass.getMethod("add", Object.class).invoke(sequence, frame);
            }

            int count = ((Number) autoCount.invoke(controller, 0, test.measurements().length - 1)).intValue();
            json.append("    {\"name\":\"").append(test.name()).append("\",\"measurements\":[");
            appendDoubles(json, test.measurements());
            json.append("],\"holyGrailRatings\":[");
            appendInts(json, test.ratings());
            json.append("],\"lrtCount\":").append(count).append("}")
                    .append(caseIndex + 1 == cases.size() ? "\n" : ",\n");
            System.out.printf("%s frames=%d LRT-count=%d%n", test.name(), test.measurements().length, count);
        }
        json.append("  ]\n}\n");
        Files.writeString(Path.of(args[0]), json.toString(), StandardCharsets.UTF_8);
    }

    private static double[] constant(int frames, double value) {
        double[] values = new double[frames];
        Arrays.fill(values, value);
        return values;
    }

    private static double[] linear(int frames, double end) {
        double[] values = new double[frames];
        for (int index = 0; index < frames; index++) {
            values[index] = end * index / (frames - 1);
        }
        return values;
    }

    private static double rangeForCount(int frames, double targetCount) {
        double frameFactor = 1 + 1000.0 / frames / 10;
        return targetCount * (double) Float.valueOf(0.1f) / frameFactor;
    }

    private static double[] step(int frames, int start, double value) {
        double[] values = constant(frames, 0);
        Arrays.fill(values, start, frames, value);
        return values;
    }

    private static int[] ratings(int frames) {
        return new int[frames];
    }

    private static int[] pairRatings(int frames, int first, int second) {
        int[] values = ratings(frames);
        values[first] = 2;
        values[second] = 3;
        return values;
    }

    private static int[] singleRating(int frames, int index, int rating) {
        int[] values = ratings(frames);
        values[index] = rating;
        return values;
    }

    private static void appendDoubles(StringBuilder output, double[] values) {
        for (int index = 0; index < values.length; index++) {
            if (index != 0) output.append(',');
            output.append(String.format(Locale.ROOT, "%.17g", values[index]));
        }
    }

    private static void appendInts(StringBuilder output, int[] values) {
        for (int index = 0; index < values.length; index++) {
            if (index != 0) output.append(',');
            output.append(values[index]);
        }
    }

    private static void copyWithoutSignatures(Path source, Path destination) throws Exception {
        try (JarFile jar = new JarFile(source.toFile(), false);
                JarOutputStream output = new JarOutputStream(Files.newOutputStream(destination))) {
            Enumeration<JarEntry> entries = jar.entries();
            byte[] buffer = new byte[64 * 1024];
            while (entries.hasMoreElements()) {
                JarEntry entry = entries.nextElement();
                if (isSignatureMetadata(entry.getName())) continue;
                output.putNextEntry(new JarEntry(entry.getName()));
                if (!entry.isDirectory()) {
                    try (var input = jar.getInputStream(entry)) {
                        int read;
                        while ((read = input.read(buffer)) >= 0) {
                            output.write(buffer, 0, read);
                        }
                    }
                }
                output.closeEntry();
            }
        }
    }

    private static boolean isSignatureMetadata(String name) {
        String upper = name.toUpperCase(Locale.ROOT);
        if (!upper.startsWith("META-INF/")) return false;
        String leaf = upper.substring("META-INF/".length());
        return leaf.equals("MANIFEST.MF") || leaf.endsWith(".SF") || leaf.endsWith(".RSA")
                || leaf.endsWith(".DSA") || leaf.endsWith(".EC") || leaf.endsWith(".SIG");
    }
}
