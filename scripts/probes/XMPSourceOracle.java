package lrtlprobe;
import java.io.*;
import java.nio.file.*;
import java.util.*;
import java.util.concurrent.TimeUnit;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of the complete native file-level XMP source reader.
// It does not construct or replace a GUI, main object, licensing state or method.
public final class XMPSourceOracle {
 public static void main(String[] args)throws Exception {
  Class.forName("tQa");Class<?> xml=Class.forName("Wd"),options=Class.forName("bCA");
  var read=method(Class.forName("nNa"),"A",xml,File.class);
  var serialize=method(Class.forName("ABA"),"A",String.class,xml,options);
  StringBuilder out=new StringBuilder("{\"file_reader_completed\":true,\"cases\":[\n");String comma="";
  try(var directories=Files.list(Path.of(args[1]))) {
   for(Path directory:directories.filter(Files::isDirectory).sorted().toList()) {
    Path image;
    try(var entries=Files.list(directory)) {image=entries.filter(p->!p.toString().endsWith(".xmp")).findFirst().orElseThrow();}
    Path sidecar=directory.resolve("frame.xmp");boolean side=Files.exists(sidecar);
    Object packet=read.invoke(null,image.toFile());
    out.append(comma).append("{\"name\":").append(quote(directory.getFileName().toString())).append(",\"filename\":").append(quote(image.getFileName().toString()));
    out.append(",\"data\":").append(quote(Base64.getEncoder().encodeToString(Files.readAllBytes(image))));
    out.append(",\"sidecar\":").append(side?quote(Files.readString(sidecar)):"null");
    out.append(",\"image_time_ms\":").append(image.toFile().lastModified());
    out.append(",\"sidecar_time_ms\":").append(side?Long.toString(sidecar.toFile().lastModified()):"null");
    out.append(",\"image_time_ns\":").append(Files.getLastModifiedTime(image).to(TimeUnit.NANOSECONDS));
    out.append(",\"sidecar_time_ns\":").append(side?Long.toString(Files.getLastModifiedTime(sidecar).to(TimeUnit.NANOSECONDS)):"null");
    out.append(",\"result\":").append(packet==null?"null":quote((String)serialize.invoke(null,packet,options.getConstructor(int.class).newInstance(16)))).append('}');comma=",\n";
   }
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
