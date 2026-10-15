package lrtlprobe;

import java.util.TreeSet;

/** Independent measurement harness; requires a user-installed LRT 7.5.5 JAR. */
public class SmoothingOracle {
    public static void main(String[] args) {
        System.out.println("[");
        boolean first = true;
        for (int length : new int[] {1, 2, 3, 5, 10, 31, 2001}) {
            for (double span : new double[] {0, .001, .05, .2, .5, 1, 1.5, 2}) {
                // A failed one-neighbor retry initializes the application's logger.
                // Skip that path so the measurement does not start the application.
                if (length > 2 && span <= 1 && (int) (span * length) == 1) continue;
                int[] indices = new int[length];
                double[] values = new double[length];
                for (int i = 0; i < length; i++) {
                    indices[i] = i;
                    values[i] = (i * 37 % 23) - 11 + (i % 7) * .125;
                }
                double[] output = lrtimelapse.eA.A(length, indices, values, span);
                if (!first) System.out.println(",");
                first = false;
                System.out.print("{\"length\":" + length + ",\"smoothing\":" + span + ",\"indices\":[");
                TreeSet<Integer> samples = new TreeSet<>();
                int count = Math.min(length, 32);
                for (int i = 0; i < count; i++) {
                    samples.add(i * (length - 1) / Math.max(1, count - 1));
                }
                String separator = "";
                for (int i : samples) {
                    System.out.print(separator + i);
                    separator = ",";
                }
                System.out.print("],\"expected\":[");
                separator = "";
                for (int i : samples) {
                    System.out.print(separator + output[i]);
                    separator = ",";
                }
                System.out.print("]}");
            }
        }
        System.out.println("\n]");
    }
}
