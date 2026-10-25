package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;

import static lrtlprobe.MetadataPasteOracle.quote;

// Calls LRT's numeric missing-column helper and registered XMP writer with
// synthetic selector-range paths. No GUI, image, application code or user XMP
// is used.
public final class MissingSelectorRangeOracle {
    static final int COLUMN = 5;
    static final String COLLECTION = "MaskGroupBasedCorrections";
    static final String PREFIX = COLLECTION + "[1]/crs:CorrectionMasks[1]/crs:CorrectionRangeMask/crs:";

    static String body(String property, String value, int frame) {
        String type = property.equals("Type") ? value : property.startsWith("Lum") ? "2" : "1";
        String amount = property.equals("ColorAmount") ? value : ".5";
        String lumMin = property.equals("LumMin") ? value : ".1";
        String lumMax = property.equals("LumMax") ? value : ".9";
        String lumFeather = property.equals("LumFeather") ? value : ".2";
        return "<crs:" + COLLECTION + "><rdf:Seq><rdf:li rdf:parseType=\"Resource\">"
            + "<crs:CorrectionMasks><rdf:Seq><rdf:li rdf:parseType=\"Resource\">"
            + "<crs:What>Mask/Aggregate</crs:What><crs:MaskSyncID>own-mask-" + frame + "</crs:MaskSyncID>"
            + "<crs:CorrectionRangeMask rdf:parseType=\"Resource\">"
            + (type == null ? "" : "<crs:Type>" + type + "</crs:Type>")
            + (amount == null ? "" : "<crs:ColorAmount>" + amount + "</crs:ColorAmount>")
            + (lumMin == null ? "" : "<crs:LumMin>" + lumMin + "</crs:LumMin>")
            + (lumMax == null ? "" : "<crs:LumMax>" + lumMax + "</crs:LumMax>")
            + (lumFeather == null ? "" : "<crs:LumFeather>" + lumFeather + "</crs:LumFeather>")
            + "<crs:PointModels>target-samples-" + frame + "</crs:PointModels>"
            + "</crs:CorrectionRangeMask></rdf:li></rdf:Seq></crs:CorrectionMasks>"
            + "</rdf:li></rdf:Seq></crs:" + COLLECTION + ">";
    }

    static String property(Object frame, String path) throws Exception {
        Field packetField = RegisteredSaveOracle.frameType.getDeclaredField("Aa");
        packetField.setAccessible(true);
        Object packet = packetField.get(frame);
        return (String) RegisteredSaveOracle.read.invoke(packet, RegisteredSaveOracle.CRS, path);
    }

    public static void main(String[] args) throws Exception {
        RegisteredSaveOracle.initialize();
        Class<?> sequenceType = RegisteredSaveOracle.sequenceType;
        Class<?> frameType = RegisteredSaveOracle.frameType;
        Class<?> xml = RegisteredSaveOracle.xml;
        Class<?> mainType = Class.forName("lrtimelapse.LRTimelapseMain");
        Object main = mainType.getConstructor().newInstance();
        Method fill = Arrays.stream(mainType.getDeclaredMethods())
            .filter(m -> m.getName().equals("A") && m.getReturnType() == boolean.class
                && Arrays.equals(m.getParameterTypes(), new Class<?>[]{sequenceType, int.class}))
            .findFirst().orElseThrow();
        fill.setAccessible(true);
        Method store = MetadataPasteOracle.method(frameType, "A", void.class, sequenceType, ArrayList.class, boolean.class);
        Method assign = MetadataPasteOracle.method(frameType, "a", void.class, int.class, Object.class);
        Field rawField = frameType.getDeclaredField("X");
        rawField.setAccessible(true);
        Field packetField = frameType.getDeclaredField("Aa");
        packetField.setAccessible(true);
        StringBuilder output = new StringBuilder("{\"cases\":[");
        String separator = "";
        LinkedHashMap<String, String[]> cases = new LinkedHashMap<>();
        cases.put("Type", new String[]{"1", null, "1"});
        cases.put("ColorAmount", new String[]{".25", null, ".75"});
        cases.put("LumMin", new String[]{".1", null, ".8"});
        cases.put("LumMax", new String[]{".2", null, ".9"});
        cases.put("LumFeather", new String[]{".15", null, ".65"});
        for (Map.Entry<String, String[]> test : cases.entrySet()) {
            String property = test.getKey();
            String path = PREFIX + property;
            Object sequence = sequenceType.getConstructor(int.class).newInstance(-1);
            MetadataPasteOracle.method(sequenceType, "d", void.class, int.class).invoke(sequence, 2);
            ArrayList<Object> frames = new ArrayList<>();
            for (int i = 0; i < test.getValue().length; i++) {
                String packetText = RegisteredSaveOracle.packet("xmp:Rating=\"4\" lrt:Quality=\"RAW\"", body(property, test.getValue()[i], i));
                Object packet = RegisteredSaveOracle.parse.invoke(null, packetText);
                Object frame = RegisteredSaveOracle.frame(packet);
                ((Object[]) rawField.get(frame))[COLUMN] = test.getValue()[i] == null ? null : Double.valueOf(test.getValue()[i]);
                sequenceType.getMethod("add", Object.class).invoke(sequence, frame);
                frames.add(frame);
            }
            boolean changed = (boolean) fill.invoke(main, sequence, COLUMN);
            for (Object frame : frames) {
                Object value = ((Object[]) rawField.get(frame))[COLUMN];
                if (value != null) assign.invoke(frame, COLUMN, value);
            }
            ArrayList<String> properties = new ArrayList<>(RegisteredSaveOracle.names);
            properties.set(COLUMN, path);
            String storeError = "";
            for (Object frame : frames) {
                try {
                    store.invoke(frame, sequence, properties, false);
                } catch (InvocationTargetException failure) {
                    Throwable cause = failure.getCause();
                    storeError = cause.toString();
                    if (!storeError.contains("LRTimelapseMain.m()")) {
                        throw new IllegalStateException("Unexpected post-store stage: " + storeError, cause);
                    }
                }
            }
            output.append(separator).append("{\"property\":").append(quote(property))
                .append(",\"path\":").append(quote(path)).append(",\"changed\":").append(changed)
                .append(",\"post_store_error\":").append(storeError.contains("LRTimelapseMain.m()"))
                .append(",\"input\":[");
            for (int i = 0; i < test.getValue().length; i++) {
                if (i > 0) output.append(',');
                String value = test.getValue()[i];
                output.append(value == null ? "null" : quote(value));
            }
            output.append("],\"filled\":[");
            for (int i = 0; i < frames.size(); i++) {
                if (i > 0) output.append(',');
                Object value = ((Object[]) rawField.get(frames.get(i)))[COLUMN];
                output.append(value == null ? "null" : quote(value.toString()));
            }
            output.append("],\"saved\":[");
            for (int i = 0; i < frames.size(); i++) {
                if (i > 0) output.append(',');
                String value = property(frames.get(i), path);
                output.append(value == null ? "null" : quote(value));
            }
            output.append("],\"point_models\":[");
            for (int i = 0; i < frames.size(); i++) {
                if (i > 0) output.append(',');
                String samples = property(frames.get(i), PREFIX + "PointModels");
                output.append(samples == null ? "null" : quote(samples));
            }
            output.append("]}");
            separator = ",";
        }
        Files.writeString(Path.of(args[0]), output.append("]}\n"));
    }
}
