package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;

// Original observer. Calls actual filename/date comparators on synthetic text
// and cache cells; no GUI, singleton, license, image file or settings writes.
public final class SequenceOrderOracle {
    static String quote(String value) {
        if (value == null) return "null";
        StringBuilder out = new StringBuilder("\"");
        for (char c : value.toCharArray()) {
            if (c == '\\' || c == '"') out.append('\\').append(c);
            else if (c < 32) out.append(String.format("\\u%04x", (int)c));
            else out.append(c);
        }
        return out.append('"').toString();
    }
    static String array(String[] values) {
        return "["+String.join(",", Arrays.stream(values).map(SequenceOrderOracle::quote).toList())+"]";
    }
    public static void main(String[] args) throws Exception {
        Class<?> naturalType = Class.forName("lrtimelapse.tools.coM9");
        Object natural = naturalType.getConstructor().newInstance();
        Method compare = naturalType.getMethod("A", String.class, String.class);
        Field pattern = naturalType.getDeclaredField("m"); pattern.setAccessible(true);
        String[] names = {"frame2.nef","frame10.nef","frame1.nef","frame01.nef","frame001.nef",
            "A.nef","a.nef","b.nef","B.nef","a_b.nef","a b.nef","a@b.nef","a!b.nef",
            "a-2.nef","a2.nef","a2b3.nef","a2b10.nef","a02b3.nef","a.2.nef","a 2.nef",
            "000.nef","0.nef","10.nef","2.nef","a","a0","a00","a000","a1","a01",
            "a9223372036854775807.nef","a9223372036854775808.nef","a000000000000000000000000002.nef",
            "é2.nef","É10.nef","İ.nef","i.nef","I.nef","ı.nef","ς.nef","σ.nef","Σ.nef",
            "あ2.nef","あ10.nef","😀2.nef","😀10.nef","𐐀.nef","𐐨.nef","\uE000.nef",
            "١٢.nef","٢.nef","１２.nef","２.nef","a\t2.nef","a\n2.nef","","_","@",
            "١٢","٢","１２","２","𝟚","𞥒"};
        StringBuilder out = new StringBuilder("{\"pattern\":").append(quote(pattern.get(null).toString())).append(",\"pairs\":[\n");
        String comma = "";
        for (String left : names) for (String right : names) {
            Integer result = null; String error = null;
            try { result = Integer.signum((Integer)compare.invoke(natural,left,right)); }
            catch (InvocationTargetException e) { error = e.getCause().getClass().getSimpleName(); }
            out.append(comma).append("{\"left\":").append(quote(left)).append(",\"right\":").append(quote(right))
                .append(",\"natural\":").append(result).append(",\"ascii\":").append(Integer.signum(left.compareToIgnoreCase(right)))
                .append(",\"error\":").append(quote(error)).append('}');
            comma=",\n";
        }
        out.append("\n],\"date_cases\":[\n");
        Class<?> frameType=Class.forName("tQa");
        int column=frameType.getField("Xa").getInt(null);
        Object dateComparator=Class.forName("CSa").getConstructor().newInstance();
        Method dateCompare=dateComparator.getClass().getMethod("A",frameType,frameType);
        String[][] dates={
            {"2026-01-01T02:00:00+09:00","2026-01-01T01:00:00Z","2026-01-01T01:00:00+01:00"},
            {"same","same","same"}, {null,null,null}, {"","",""},
            {"a","A","a"}, {"9","10","2"}, {"😀","\uE000","あ"},
            {"2026-01-01","invalid","2025-12-31"}
        };
        String[] dateNames={"frame2.nef","frame10.nef","frame1.nef"};
        comma="";
        for (String[] ds:dates) {
            List<Object> frames=new ArrayList<>(); IdentityHashMap<Object,Integer> ids=new IdentityHashMap<>();
            for(int i=0;i<ds.length;i++) {
                Object[] raw=new Object[4096];raw[0]=dateNames[i];raw[column]=ds[i];
                Object frame=frameType.getConstructor(Object[].class).newInstance((Object)raw);frames.add(frame);ids.put(frame,i);
            }
            frames.sort((a,b)->{try{return (Integer)dateCompare.invoke(dateComparator,a,b);}catch(Exception e){throw new RuntimeException(e);}});
            out.append(comma).append("{\"names\":").append(array(dateNames)).append(",\"dates\":").append(array(ds)).append(",\"order\":[");
            for(int i=0;i<frames.size();i++){if(i>0)out.append(',');out.append(ids.get(frames.get(i)));}
            out.append("]}");comma=",\n";
        }
        Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
    }
}
