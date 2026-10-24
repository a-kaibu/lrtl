package lrtlprobe;
import java.io.*;
import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer: genuine CSV reader fed installed ExifTool's output.
public final class CaptureDateImageOracle {
 public static void main(String[] args)throws Exception {
  Class<?> frame=Class.forName("tQa"),registry=Class.forName("Ooa"),controller=Class.forName("lrtimelapse.cOM7"),unsafe=Class.forName("sun.misc.Unsafe");
  Object columns=method(registry,"A",registry).invoke(null);int count=(int)method(registry,"A",int.class).invoke(columns);
  Field singleton=unsafe.getDeclaredField("theUnsafe");singleton.setAccessible(true);
  Object shell=unsafe.getMethod("allocateInstance",Class.class).invoke(singleton.get(null),controller);
  Method metadata=method(controller,"A",void.class,String.class,frame),read=method(frame,"C",String.class);
  StringBuilder out=new StringBuilder("{\"metadata_completed\":true,\"cases\":[\n");String comma="";
  for(String line:Files.readAllLines(Path.of(args[1]))) {
   // The generated SourceFile has no commas or quotes; the other 15 values are
   // synthetic dates/dashes. The native reader itself parses the actual CSV.
   String path=line.substring(0,line.indexOf(','));Path image=Path.of(path);
   Object f=frame.getConstructor(Object[].class).newInstance((Object)new Object[count]);metadata.invoke(shell,line,f);
   Object seeded=frame.getConstructor(Object[].class).newInstance((Object)new Object[count]);
   method(frame,"h",void.class,String.class).invoke(seeded,"2031-01-01T00:00:00");metadata.invoke(shell,line,seeded);
   out.append(comma).append("{\"name\":").append(quote(image.getFileName().toString())).append(",\"data\":").append(quote(Base64.getEncoder().encodeToString(Files.readAllBytes(image)))).append(",\"date\":").append(quote((String)read.invoke(f))).append(",\"seeded_date\":").append(quote((String)read.invoke(seeded))).append('}');comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
