package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of the complete native CSV metadata reader and date helper.
// The uninitialized controller has no UI/main/license substitutes; this reader
// only uses its CSV argument and frame. No application methods are replaced.
public final class CaptureDateCacheOracle {
 public static void main(String[] args)throws Exception {
  Locale.setDefault(Locale.JAPAN);
  Class<?> frame=Class.forName("tQa"),registry=Class.forName("Ooa"),controller=Class.forName("lrtimelapse.cOM7"),unsafe=Class.forName("sun.misc.Unsafe");
  Object columns=method(registry,"A",registry).invoke(null);
  int count=(int)method(registry,"A",int.class).invoke(columns);
  System.out.println("Capture date column: "+method(registry,"a",String.class,int.class).invoke(columns,frame.getField("Xa").getInt(null)));
  Field singleton=unsafe.getDeclaredField("theUnsafe");singleton.setAccessible(true);
  Object shell=unsafe.getMethod("allocateInstance",Class.class).invoke(singleton.get(null),controller);
  Method metadata=method(controller,"A",void.class,String.class,frame),read=method(frame,"C",String.class),set=method(frame,"h",void.class,String.class);
  StringBuilder out=new StringBuilder("{\"metadata_completed\":true,\"cases\":[\n");String comma="";
  String[] dates={"-","","bad","2026:10:24 03:04:05","2026:10:24 03:04:05.1","2026:10:24 03:04:05.1234","2026-10-24T03:04:05.123+09:00","2026-10-24 03:04:05"," 2026:10:24 03:04:05 ","\"2026:10:24 03:04:05\""};
  String[] subs={"-","","2026:10:24 03:04:05.1","2026:10:24 03:04:05.12","2026:10:24 03:04:05.123456+09:00","2026:10:24 03:04:05.1+09:00","2026:10:24 03:04:05.1-09:00","2026:10:24 03:04:05.1Z","2026:10:24 03:04:05.12Z","2026:10:24 03:04:05.1 ","2026:10:24 03:04:05,123"};
  for(String seed:new String[]{null,"","seed","2031-01-01T00:00:00.123456+09:00"})for(String date:dates)for(String sub:subs) {
   Object f=frame.getConstructor(Object[].class).newInstance((Object)new Object[count]);
   if(seed!=null)set.invoke(f,seed);
   String[] fields=new String[16];Arrays.fill(fields,"-");fields[0]="frame.jpg";fields[1]="64x32";fields[2]=date;fields[9]="2001:02:03 04:05:06";fields[12]=sub;
   StringJoiner csv=new StringJoiner(",");for(String field:fields)csv.add("\""+field.replace("\"","\"\"")+"\"");
   metadata.invoke(shell,csv.toString(),f);
   out.append(comma).append("{\"seed\":").append(seed==null?"null":quote(seed)).append(",\"date\":").append(quote(date)).append(",\"subsecond\":").append(quote(sub)).append(",\"fallback\":\"2001:02:03 04:05:06\",\"expected\":").append(quote((String)read.invoke(f))).append('}');comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
