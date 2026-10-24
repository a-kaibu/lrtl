package lrtlprobe;

import java.io.File;
import java.nio.file.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of genuine basename/type and cache path helpers. No GUI,
// main instance, licensing object or product method is replaced; no image data
// or settings are written. This observes path calculation, not a completed copy.
public final class SequenceCompanionsOracle {
 public static void main(String[] args)throws Exception {
  Class<?> main=Class.forName("lrtimelapse.LRTimelapseMain"),paths=Class.forName("lrtimelapse.tools.cOm7");
  Path source=Path.of(args[1]).toAbsolutePath().resolve("sequence");
  StringBuilder out=new StringBuilder("{\"path_helpers_completed\":true,\"cases\":[\n");String comma="";
  for(String name:new String[]{"frame.nef","frame.NEF","frame.cr2","frame.cr3","frame.raf","frame.rw2","frame.iiq","frame.dng","frame.DNG","frame.jpg","frame.JPG","frame.jpeg","frame.JPEG","frame.tif","frame.tiff","frame.png","frame.PnG","many.parts.nef",".hidden.nef","frame01.nef","frame 01.nef","日本語.nef","frame.nef.xmp","name-without-extension"}) {
   File image=source.resolve(name).toFile();
   String stem=(String)method(paths,"a",String.class,File.class).invoke(null,image);
   boolean jpeg=(boolean)method(paths,"E",boolean.class,File.class).invoke(null,image);
   out.append(comma).append("{\"name\":").append(quote(name)).append(",\"stem\":").append(quote(stem)).append(",\"paired_jpeg\":").append(!jpeg).append(",\"cache\":[");String sep="";
   for(String helper:new String[]{"h","j","d"}) {
    File path=(File)method(main,helper,File.class,File.class).invoke(null,image);
    out.append(sep).append(quote(source.relativize(path.toPath()).toString().replace('\\','/')));sep=",";
   }
   out.append("]}");comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
