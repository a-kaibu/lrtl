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
    private record QuantitySweepCase(String name, int frames, double offset, double range, int stepAt,
            int[] ratingIndices, int[] ratings) {}
    private record PlacementCase(String name, int frames, int count, int rating, int[] initialRatings) {}
    private record SelectedRowCase(String name, int frames, int count, int rating, int[] rowIndices,
            int[] initialRatings) {}

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
        Field activeFolder = controllerClass.getDeclaredField("n");
        activeFolder.setAccessible(true);
        activeFolder.set(controller, new java.io.File("keyframe-oracle-controller"));
        Class<?> mainClass = Class.forName("lrtimelapse.LRTimelapseMain", true, lrtLoader);
        Field activeController = mainClass.getDeclaredField("N");
        activeController.setAccessible(true);
        activeController.set(null, controller);

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
        Method measurementRange = Arrays.stream(controllerClass.getMethods())
                .filter(method -> method.getName().equals("A") && method.getReturnType() == double.class
                        && method.getParameterCount() == 0)
                .findFirst().orElseThrow();
        Method applyWizardPositions = controllerClass.getDeclaredMethod(
                "A", sequenceClass, int.class, int[].class, int.class, boolean.class);
        applyWizardPositions.setAccessible(true);

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
            int[] wizardRatings = runWizard(controller, sequenceClass, frameClass, frameConstructor,
                    setRating, applyWizardPositions, test.measurements().length, count);
            int[] lrtIndices = indicesWithRating(wizardRatings, 4);
            json.append("    {\"name\":\"").append(test.name()).append("\",\"measurements\":[");
            appendDoubles(json, test.measurements());
            json.append("],\"holyGrailRatings\":[");
            appendInts(json, test.ratings());
            json.append("],\"lrtCount\":").append(count).append(",\"lrtIndices\":[");
            appendInts(json, lrtIndices);
            json.append("]}")
                    .append(caseIndex + 1 == cases.size() ? "\n" : ",\n");
            System.out.printf("%s frames=%d LRT-count=%d%n", test.name(), test.measurements().length, count);
        }

        List<QuantitySweepCase> sweepCases = quantitySweepCases();
        json.append("  ],\n  \"quantitySweepCases\": [\n");
        for (int caseIndex = 0; caseIndex < sweepCases.size(); caseIndex++) {
            QuantitySweepCase test = sweepCases.get(caseIndex);
            sequenceClass.getMethod("clear").invoke(sequence);
            double[] measurements = measurements(test);
            int[] ratings = new int[test.frames()];
            for (int marker = 0; marker < test.ratingIndices().length; marker++) {
                ratings[test.ratingIndices()[marker]] = test.ratings()[marker];
            }
            for (int index = 0; index < measurements.length; index++) {
                Object frame = frameConstructor.newInstance(String.format(Locale.ROOT, "sweep-%05d.jpg", index));
                setMeasurement.invoke(frame, measurements[index]);
                if (ratings[index] != 0) setRating.invoke(frame, ratings[index]);
                sequenceClass.getMethod("add", Object.class).invoke(sequence, frame);
            }
            double lrtRange = ((Number) measurementRange.invoke(controller)).doubleValue();
            int count = ((Number) autoCount.invoke(controller, 0, measurements.length - 1)).intValue();
            int[] wizardRatings = runWizard(controller, sequenceClass, frameClass, frameConstructor,
                    setRating, applyWizardPositions, measurements.length, count);
            int[] indices = indicesWithRating(wizardRatings, 4);
            json.append("    {\"name\":\"").append(test.name()).append("\",\"frames\":")
                    .append(test.frames()).append(",\"offset\":")
                    .append(String.format(Locale.ROOT, "%.17g", test.offset())).append(",\"range\":")
                    .append(String.format(Locale.ROOT, "%.17g", test.range())).append(",\"stepAt\":")
                    .append(test.stepAt()).append(",\"ratingIndices\":[");
            appendInts(json, test.ratingIndices());
            json.append("],\"ratings\":[");
            appendInts(json, test.ratings());
            json.append("],\"lrtRange\":").append(String.format(Locale.ROOT, "%.17g", lrtRange))
                    .append(",\"lrtCount\":").append(count).append(",\"lrtIndices\":[");
            appendInts(json, indices);
            json.append("]}").append(caseIndex + 1 == sweepCases.size() ? "\n" : ",\n");
        }
        System.out.printf("quantity boundary sweep cases=%d%n", sweepCases.size());

        json.append("  ],\n  \"placementCases\": [\n");
        List<PlacementCase> placementCases = List.of(
                new PlacementCase("interior_hg_collision_shifts_one_frame", 10, 3, 4,
                        singleRating(10, 5, 2)),
                new PlacementCase("collision_search_accepts_ninth_preceding_frame", 30, 3, 4,
                        occupiedBefore(30, 15, 8)),
                new PlacementCase("collision_with_no_empty_frame_in_nine", 30, 3, 4,
                        occupiedBefore(30, 15, 9)),
                new PlacementCase("same_target_rating_is_replaced", 10, 3, 4,
                        singleRating(10, 5, 4)),
                new PlacementCase("opposite_keyframe_rating_at_endpoint", 10, 3, 4,
                        singleRating(10, 0, 5)),
                new PlacementCase("holy_grail_endpoint_is_overwritten_by_lrt", 10, 3, 4,
                        singleRating(10, 0, 2)),
                new PlacementCase("crop_target_shifts_default_keyframe", 10, 3, 5,
                        singleRating(10, 5, 4)),
                new PlacementCase("crop_target_replaces_default_at_endpoint", 10, 3, 5,
                        singleRating(10, 9, 4))
        );
        for (int caseIndex = 0; caseIndex < placementCases.size(); caseIndex++) {
            PlacementCase test = placementCases.get(caseIndex);
            int[] lrtRatings = runWizard(controller, sequenceClass, frameClass, frameConstructor,
                    setRating, applyWizardPositions, test.frames(), test.count(), test.rating(), test.initialRatings());
            json.append("    {\"name\":\"").append(test.name()).append("\",\"count\":")
                    .append(test.count()).append(",\"targetRating\":").append(test.rating())
                    .append(",\"initialRatings\":[");
            appendInts(json, test.initialRatings());
            json.append("],\"lrtRatings\":[");
            appendInts(json, lrtRatings);
            json.append("]}").append(caseIndex + 1 == placementCases.size() ? "\n" : ",\n");
            System.out.printf("%s frames=%d count=%d rating=%d%n", test.name(), test.frames(), test.count(), test.rating());
        }

        json.append("  ],\n  \"selectedRowCases\": [\n");
        int[] selectedEndpointRatings = singleRating(20, 2, 2);
        selectedEndpointRatings[19] = 3;
        List<SelectedRowCase> selectedRowCases = List.of(
                new SelectedRowCase("sparse_rows_count_four", 40, 4, 4,
                        new int[]{1, 5, 8, 14, 21, 27, 33, 38}, ratings(40)),
                new SelectedRowCase("sparse_rows_count_two", 30, 2, 4,
                        new int[]{2, 7, 15, 28}, ratings(30)),
                new SelectedRowCase("count_exceeds_selected_row_count", 16, 5, 4,
                        new int[]{1, 6, 11}, ratings(16)),
                new SelectedRowCase("single_count_marks_first_selected_row", 20, 1, 4,
                        new int[]{3, 8, 16}, ratings(20)),
                new SelectedRowCase("holy_grail_selected_endpoint_overwrites_rating", 20, 3, 4,
                        new int[]{2, 8, 14, 19}, selectedEndpointRatings),
                new SelectedRowCase("holy_grail_collision_shifts_before_selected_row", 20, 4, 4,
                        new int[]{2, 8, 14, 19}, singleRating(20, 14, 3))
        );
        for (int caseIndex = 0; caseIndex < selectedRowCases.size(); caseIndex++) {
            SelectedRowCase test = selectedRowCases.get(caseIndex);
            int[] lrtRatings = runWizard(controller, sequenceClass, frameClass, frameConstructor,
                    setRating, applyWizardPositions, test.frames(), test.count(), test.rating(),
                    test.initialRatings(), test.rowIndices());
            json.append("    {\"name\":\"").append(test.name()).append("\",\"frames\":")
                    .append(test.frames()).append(",\"count\":").append(test.count())
                    .append(",\"targetRating\":").append(test.rating()).append(",\"rowIndices\":[");
            appendInts(json, test.rowIndices());
            json.append("],\"initialRatings\":[");
            appendInts(json, test.initialRatings());
            json.append("],\"lrtRatings\":[");
            appendInts(json, lrtRatings);
            json.append("]}").append(caseIndex + 1 == selectedRowCases.size() ? "\n" : ",\n");
            System.out.printf("%s frames=%d count=%d selected=%d%n", test.name(), test.frames(),
                    test.count(), test.rowIndices().length);
        }
        json.append("  ]\n}\n");
        Files.writeString(Path.of(args[0]), json.toString(), StandardCharsets.UTF_8);
    }

    private static List<QuantitySweepCase> quantitySweepCases() {
        int[] frameCounts = {2, 3, 49, 50, 51, 99, 100, 149, 150, 249, 250, 251, 499, 500, 501,
                999, 1000, 1001, 1249, 1250, 1251, 2499, 2500};
        double[] baseTargets = {0, 0.5, 0.9999999, 1, 1.0000001, 1.9999999, 2, 2.0000001,
                3.9999999, 4, 4.0000001, 5.9999999, 6, 6.0000001, 9.999999, 10, 10.000001,
                19.999999, 20, 20.000001, 39.999999, 40, 40.000001, 49.999999, 50, 50.000001};
        List<QuantitySweepCase> result = new ArrayList<>();
        for (int frames : frameCounts) {
            List<Double> targets = new ArrayList<>();
            for (double target : baseTargets) targets.add(target);
            double minimumAnchorTarget = (frames - 1) / 250.0;
            double spacingCapTarget = (frames - 1) / 50.0;
            for (double boundary : new double[]{minimumAnchorTarget, spacingCapTarget}) {
                targets.add(Math.max(0, boundary - 0.000001));
                targets.add(boundary);
                targets.add(boundary + 0.000001);
            }
            for (int targetIndex = 0; targetIndex < targets.size(); targetIndex++) {
                double target = targets.get(targetIndex);
                double offset = switch (targetIndex % 3) {
                    case 1 -> -8.25;
                    case 2 -> 6.5;
                    default -> 0;
                };
                result.add(new QuantitySweepCase(
                        String.format(Locale.ROOT, "linear_n%d_t%03d", frames, targetIndex),
                        frames, offset, rangeForCount(frames, target), -1, new int[0], new int[0]));
            }
        }

        result.add(new QuantitySweepCase("adjacent_2_3_pair_cancels_step", 500, 0, 2, 250,
                new int[]{249, 250}, new int[]{2, 3}));
        result.add(new QuantitySweepCase("adjacent_2_2_pair_cancels_step", 500, 0, 2, 250,
                new int[]{249, 250}, new int[]{2, 2}));
        result.add(new QuantitySweepCase("adjacent_3_2_pair_cancels_step", 500, 0, 2, 250,
                new int[]{249, 250}, new int[]{3, 2}));
        result.add(new QuantitySweepCase("adjacent_3_3_pair_cancels_step", 500, 0, 2, 250,
                new int[]{249, 250}, new int[]{3, 3}));
        result.add(new QuantitySweepCase("nonadjacent_2_3_does_not_cancel_step", 500, 0, 2, 250,
                new int[]{249, 251}, new int[]{2, 3}));
        result.add(new QuantitySweepCase("intervening_four_star_breaks_pair", 500, 0, 2, 250,
                new int[]{249, 250, 251}, new int[]{2, 4, 3}));
        result.add(new QuantitySweepCase("ordinary_four_five_stars_do_not_cancel_step", 500, 0, 2, 250,
                new int[]{249, 250}, new int[]{4, 5}));
        result.add(new QuantitySweepCase("single_two_star_raises_flat_quantity", 500, 0, 0, -1,
                new int[]{250}, new int[]{2}));
        result.add(new QuantitySweepCase("single_three_star_raises_flat_quantity", 500, 0, 0, -1,
                new int[]{250}, new int[]{3}));
        result.add(new QuantitySweepCase("single_four_star_does_not_raise_flat_quantity", 500, 0, 0, -1,
                new int[]{250}, new int[]{4}));
        result.add(new QuantitySweepCase("single_five_star_does_not_raise_flat_quantity", 500, 0, 0, -1,
                new int[]{250}, new int[]{5}));
        return result;
    }

    private static double[] measurements(QuantitySweepCase test) {
        double[] values = new double[test.frames()];
        if (test.stepAt() >= 0) {
            for (int index = test.stepAt(); index < values.length; index++) {
                values[index] = test.offset() + test.range();
            }
        } else {
            for (int index = 0; index < values.length; index++) {
                values[index] = test.offset() + test.range() * index / (values.length - 1);
            }
        }
        return values;
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

    private static int[] occupiedBefore(int frames, int collisionIndex, int occupiedPrecedingFrames) {
        int[] values = singleRating(frames, collisionIndex, 2);
        int[] occupiedRatings = {2, 3, 5};
        for (int distance = 1; distance <= occupiedPrecedingFrames; distance++) {
            values[collisionIndex - distance] = occupiedRatings[distance % occupiedRatings.length];
        }
        return values;
    }

    private static int[] indicesWithRating(int[] ratings, int rating) {
        int[] indices = new int[ratings.length];
        int count = 0;
        for (int index = 0; index < ratings.length; index++) {
            if (ratings[index] == rating) indices[count++] = index;
        }
        return Arrays.copyOf(indices, count);
    }

    private static int[] runWizard(Object controller, Class<?> sequenceClass, Class<?> frameClass,
            Constructor<?> frameConstructor, Method setRating, Method applyWizardPositions, int frameCount,
            int requestedCount) throws Exception {
        return runWizard(controller, sequenceClass, frameClass, frameConstructor, setRating,
                applyWizardPositions, frameCount, requestedCount, 4, ratings(frameCount));
    }

    private static int[] runWizard(Object controller, Class<?> sequenceClass, Class<?> frameClass,
            Constructor<?> frameConstructor, Method setRating, Method applyWizardPositions, int frameCount,
            int requestedCount, int requestedRating, int[] initialRatings) throws Exception {
        return runWizard(controller, sequenceClass, frameClass, frameConstructor, setRating,
                applyWizardPositions, frameCount, requestedCount, requestedRating, initialRatings, null);
    }

    private static int[] runWizard(Object controller, Class<?> sequenceClass, Class<?> frameClass,
            Constructor<?> frameConstructor, Method setRating, Method applyWizardPositions, int frameCount,
            int requestedCount, int requestedRating, int[] initialRatings, int[] requestedRows) throws Exception {
        Object sequence = sequenceClass.getConstructor().newInstance();
        Method add = sequenceClass.getMethod("add", Object.class);
        Method getRating = Arrays.stream(frameClass.getMethods())
                .filter(method -> method.getName().equals("C") && method.getParameterCount() == 0
                        && method.getReturnType() == int.class)
                .findFirst().orElseThrow();
        int[] allIndices = new int[frameCount];
        Object[] frames = new Object[frameCount];
        for (int index = 0; index < frameCount; index++) {
            allIndices[index] = index;
            Object frame = frameConstructor.newInstance(String.format(Locale.ROOT, "placement-%05d.jpg", index));
            setRating.invoke(frame, initialRatings[index]);
            frames[index] = frame;
            add.invoke(sequence, frame);
        }
        int[] rows = requestedRows == null ? allIndices : requestedRows;
        applyWizardPositions.invoke(controller, sequence, requestedCount, rows, requestedRating, false);
        int[] result = new int[frameCount];
        for (int index = 0; index < frameCount; index++) {
            result[index] = ((Number) getRating.invoke(frames[index])).intValue();
        }
        return result;
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
