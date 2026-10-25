package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;
import java.util.concurrent.ConcurrentSkipListSet;
import javax.swing.*;
import sun.misc.Unsafe;

// Runs the installed LRT long-term panel's actual luminance, contrast, and
// hue workers in their native order, without showing a window or reading image
// files. The stored measurements are synthetic and isolate selection logic.
public final class LongTermWorkerOracle {
    static Unsafe unsafe() throws Exception {
        Field f = Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        return (Unsafe) f.get(null);
    }

    static void set(Object target, Class<?> owner, String name, Object value) throws Exception {
        Field f = owner.getDeclaredField(name);
        f.setAccessible(true);
        f.set(target, value);
    }

    static Method method(Class<?> c, String name, Class<?> result, Class<?>... parameters) {
        return Arrays.stream(c.getDeclaredMethods())
            .filter(m -> m.getName().equals(name) && m.getReturnType() == result
                && Arrays.equals(m.getParameterTypes(), parameters))
            .findFirst().map(m -> { m.setAccessible(true); return m; })
            .orElseThrow();
    }

    static Method modelSequence(Class<?> model, Class<?> sequence) {
        return Arrays.stream(model.getMethods())
            .filter(m -> m.getName().equals("A") && m.getParameterCount() == 0
                && m.getReturnType() == sequence)
            .findFirst().orElseThrow();
    }

    static double[] metric(int count, int pattern, int axis) {
        double[] values = new double[count];
        for (int i = 0; i < count; i++) {
            double position = i / (double) Math.max(1, count - 1);
            double value;
            switch (pattern) {
                case 0: value = .4; break;
                case 1: value = .12 + .76 * position; break;
                case 2:
                    value = i == count / (axis + 2) ? .96 : .24 + .02 * (i % 4);
                    break;
                case 3: value = (i & 1) == 0 ? .18 + .03 * axis : .82 - .03 * axis; break;
                case 4: value = i < count / 2 ? .94 - .02 * axis : .06 + .02 * axis; break;
                default:
                    value = .43 + .17 * Math.sin(i * (.57 + .13 * axis))
                        + ((i + axis) % 5 == 1 ? .19 : 0);
                    break;
            }
            values[i] = Math.max(0, Math.min(1, value));
        }
        return values;
    }

    static Object slider(int minimum, int maximum, int value) {
        return new JSlider(minimum, maximum, value);
    }

    static void setAxis(Object filter, Class<?> panel, String checkbox, String offset,
                        String smoothing, String low, String high, boolean constant,
                        int offsetValue, int smoothValue, int lowValue, int highValue) throws Exception {
        JCheckBox toggle = new JCheckBox();
        toggle.setSelected(constant);
        set(filter, panel, checkbox, toggle);
        set(filter, panel, offset, slider(-100, 100, offsetValue));
        set(filter, panel, smoothing, slider(0, 50, smoothValue));
        set(filter, panel, low, slider(0, 100, lowValue));
        set(filter, panel, high, slider(0, 100, highValue));
    }

    static int[] axisSettings(int config, int pattern, int axis) {
        if (config == 0) return new int[]{25 - axis * 4, 50, 50, 0, 0};
        if (config == 1) {
            int[][] thresholds = {{20, 70}, {10, 80}, {35, 60}};
            return new int[]{50, thresholds[axis][0], thresholds[axis][1], 0, 0};
        }
        if (config == 2) {
            int[] offsets = {-75, 0, 75};
            return new int[]{0, 40, 60, offsets[(pattern + axis) % offsets.length], 1};
        }
        return new int[]{0, 50, 50, 0, 0};
    }

    static String number(double value) {
        return Double.isFinite(value) ? Double.toString(value) : "null";
    }

    static String number(Double value) {
        return value == null ? "null" : number(value.doubleValue());
    }

    static String array(double[] values) {
        StringJoiner out = new StringJoiner(",", "[", "]");
        for (double value : values) out.add(number(value));
        return out.toString();
    }

    static String targets(Vector<?> frames, Class<?> frame, String getter) throws Exception {
        Method read = method(frame, getter, Double.class);
        StringJoiner out = new StringJoiner(",", "[", "]");
        for (Object image : frames) out.add(number((Double) read.invoke(image)));
        return out.toString();
    }

    static String indices(Set<Integer> selected) {
        return selected.toString();
    }

    public static void main(String[] args) throws Exception {
        Class.forName("tQa");
        Class<?> tableClass = Class.forName("lrtimelapse.cOM7");
        Class<?> modelClass = Class.forName("lrtimelapse.com8");
        Class<?> panelClass = Class.forName("Dra");
        Class<?> frameClass = Class.forName("tQa");
        Class<?> sequenceClass = Class.forName("Rsa");

        Object model = modelClass.getConstructor(ArrayList.class).newInstance(new ArrayList<>());
        Object sequence = modelSequence(modelClass, sequenceClass).invoke(model);
        Object table = unsafe().allocateInstance(tableClass);
        set(table, tableClass, "wc", model);
        set(table, tableClass, "Ya", Class.forName("lrtimelapse.com7").getMethod("A").invoke(null));

        Class<?> main = Class.forName("lrtimelapse.LRTimelapseMain");
        Field licenseState = main.getDeclaredField("d");
        licenseState.setAccessible(true);
        licenseState.set(null, Class.forName("lrtimelapse.tools.CoM3").getConstructor().newInstance());

        Object filter = panelClass.getConstructor(tableClass).newInstance(table);
        Class<?> progressClass = Class.forName("lrtimelapse.tools.Prn");
        Object progress = Class.forName("ssa").getConstructors()[0].newInstance(
            filter, "probe", null, false, new ConcurrentSkipListSet<Integer>());
        Method luminance = method(panelClass, "d", void.class, Set.class, progressClass);
        Method contrast = method(panelClass, "K", void.class, Set.class, progressClass);
        Method hue = method(panelClass, "A", void.class, Set.class, progressClass);

        StringBuilder out = new StringBuilder("{\"version\":1,\"cases\":[\n");
        int cases = 0;
        for (int count : new int[]{1, 2, 5, 7, 8, 12, 33}) {
            for (int pattern = 0; pattern < 6; pattern++) {
                for (int config = 0; config < 4; config++) {
                    @SuppressWarnings("unchecked")
                    Vector<Object> frames = (Vector<Object>) sequence;
                    frames.clear();
                    double[][] metrics = new double[3][];
                    for (int axis = 0; axis < 3; axis++) metrics[axis] = metric(count, pattern, axis);
                    for (int i = 0; i < count; i++) {
                        Object image = frameClass.getConstructor(String.class).newInstance("frame" + i + ".jpg");
                        set(image, frameClass, "O", Double.valueOf(metrics[0][i]));
                        set(image, frameClass, "K", Double.valueOf(metrics[1][i]));
                        set(image, frameClass, "f", Double.valueOf(metrics[2][i]));
                        frames.add(image);
                    }
                    int[] lum = axisSettings(config, pattern, 0);
                    int[] contrastSettings = axisSettings(config, pattern, 1);
                    int[] hueSettings = axisSettings(config, pattern, 2);
                    setAxis(filter, panelClass, "S", "fa", "u", "ja", "M",
                        lum[4] != 0, lum[3], lum[0], lum[1], lum[2]);
                    setAxis(filter, panelClass, "O", "o", "La", "Ga", "t",
                        contrastSettings[4] != 0, contrastSettings[3], contrastSettings[0], contrastSettings[1], contrastSettings[2]);
                    setAxis(filter, panelClass, "Ma", "Da", "i", "Ka", "x",
                        hueSettings[4] != 0, hueSettings[3], hueSettings[0], hueSettings[1], hueSettings[2]);

                    ConcurrentSkipListSet<Integer> selected = new ConcurrentSkipListSet<>();
                    for (int i = 0; i < count; i++) selected.add(i);
                    luminance.invoke(filter, selected, progress);
                    String afterLum = indices(selected);
                    if (config != 3) contrast.invoke(filter, selected, progress);
                    String afterContrast = indices(selected);
                    if (config != 3) hue.invoke(filter, selected, progress);
                    String afterHue = indices(selected);
                    boolean colorActive = config != 3;

                    if (cases++ > 0) out.append(",\n");
                    out.append("{\"count\":").append(count)
                        .append(",\"pattern\":").append(pattern)
                        .append(",\"config\":").append(config)
                        .append(",\"colorActive\":").append(colorActive)
                        .append(",\"metrics\":[");
                    for (int i = 0; i < count; i++) {
                        if (i > 0) out.append(',');
                        out.append("{\"luminance\":").append(number(metrics[0][i]))
                            .append(",\"contrast\":").append(number(metrics[1][i]))
                            .append(",\"hue\":").append(number(metrics[2][i])).append('}');
                    }
                    out.append("],\"preset\":{")
                        .append("\"lumSmoothing\":").append(lum[0]).append(",\"lumLow\":").append(lum[1])
                        .append(",\"lumHigh\":").append(lum[2]).append(",\"lumConstOffset\":").append(lum[3])
                        .append(",\"lumConst\":").append(lum[4] != 0)
                        .append(",\"contrastSmoothing\":").append(contrastSettings[0]).append(",\"contrastLow\":").append(contrastSettings[1])
                        .append(",\"contrastHigh\":").append(contrastSettings[2]).append(",\"contrastConstOffset\":").append(contrastSettings[3])
                        .append(",\"contrastConst\":").append(contrastSettings[4] != 0)
                        .append(",\"hueSmoothing\":").append(hueSettings[0]).append(",\"hueLow\":").append(hueSettings[1])
                        .append(",\"hueHigh\":").append(hueSettings[2]).append(",\"hueConstOffset\":").append(hueSettings[3])
                        .append(",\"hueConst\":").append(hueSettings[4] != 0)
                        .append(",\"timeFilterActive\":false,\"startHour\":0,\"startMin\":0,\"endHour\":24,\"endMin\":0")
                        .append(",\"dayMon\":true,\"dayTue\":true,\"dayWed\":true,\"dayThu\":true,\"dayFri\":true,\"daySat\":true,\"daySun\":true}")
                        .append(",\"selectedAfterLuminance\":").append(afterLum)
                        .append(",\"selectedAfterContrast\":").append(afterContrast)
                        .append(",\"selectedAfterHue\":").append(afterHue)
                        .append(",\"luminanceTargets\":").append(targets(frames, frameClass, "D"))
                        .append(",\"contrastTargets\":").append(targets(frames, frameClass, "M"))
                        .append(",\"hueTargets\":").append(targets(frames, frameClass, "I"))
                        .append('}');
                }
            }
        }
        out.append("\n]}\n");
        Files.writeString(Path.of(args[0]), out.toString());
        System.out.println("Observed native long-term worker selections: " + cases);
    }
}
