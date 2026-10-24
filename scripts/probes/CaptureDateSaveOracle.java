package lrtlprobe;
import java.nio.file.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of capture-date writes in the genuine pre-history save.
// History stops at the absent main instance, as in RegisteredSaveOracle.
public final class CaptureDateSaveOracle {
 public static void main(String[] args)throws Exception {
  RegisteredSaveOracle.initialize();
  StringBuilder out=new StringBuilder("{\"store_completed\":false,\"stop\":\"history: null LRTimelapseMain.m()\",\"cases\":[\n");String comma="";
  for(String quality:new String[]{"RAW","FLAT"})for(boolean active:new boolean[]{false,true})for(String date:new String[]{null,"","n/a","seed","2026-10-24T03:04:05.12"}) {
   String target=RegisteredSaveOracle.packet("lrt:Quality=\""+quality+"\" crs:HasSettings=\""+(active?"True":"False")+"\"", "<exif:DateTimeOriginal>2030-01-02T03:04:05</exif:DateTimeOriginal><xmp:CreateDate>2031-01-02T03:04:05</xmp:CreateDate><xmp:ModifyDate>2032-01-02T03:04:05</xmp:ModifyDate><p:DateCreated xmlns:p=\"http://ns.adobe.com/photoshop/1.0/\">2033-01-02</p:DateCreated>");
   Object packet=RegisteredSaveOracle.parse.invoke(null,target),frame=RegisteredSaveOracle.frame(packet);
   method(RegisteredSaveOracle.frameType,"h",void.class,String.class).invoke(frame,date);
   Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);
   method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);
   String saved=RegisteredSaveOracle.save(frame,packet,sequence);
   Object result=RegisteredSaveOracle.parse.invoke(null,saved);
   String clock=(String)RegisteredSaveOracle.read.invoke(result,"http://ns.adobe.com/xap/1.0/","MetadataDate");
   method(RegisteredSaveOracle.xml,"A",void.class,String.class,String.class).invoke(result,"http://ns.adobe.com/xap/1.0/","MetadataDate");
   saved=(String)RegisteredSaveOracle.serialize.invoke(null,result,RegisteredSaveOracle.options.getConstructor(int.class).newInstance(16));
   out.append(comma).append("{\"quality\":").append(quote(quality)).append(",\"active\":").append(active).append(",\"date\":").append(date==null?"null":quote(date)).append(",\"target\":").append(quote(target)).append(",\"saved\":").append(quote(saved)).append(",\"metadata_date_written\":").append(clock!=null).append('}');comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
