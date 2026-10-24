package lrtlprobe;
import java.io.File;
import java.lang.reflect.*;
import java.nio.file.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of the completed frame-specific default packet factory.
// Uses synthetic filenames and normal product objects; no main/license/method
// or preset contents are substituted, and no preferences are saved.
public final class ReloadDefaultsOracle {
 static Object defaults(String filename,String quality)throws Exception {
  Object frame=RegisteredSaveOracle.frameType.getConstructor(Object[].class).newInstance((Object)new Object[RegisteredSaveOracle.names.size()]);
  method(RegisteredSaveOracle.frameType,"K",void.class,String.class).invoke(frame,quality);
  method(RegisteredSaveOracle.frameType,"a",void.class,File.class).invoke(frame,new File(System.getProperty("java.io.tmpdir"),filename));
  RegisteredSaveOracle.assign.invoke(frame,RegisteredSaveOracle.frameType.getField("aa").getInt(null),filename);
  Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);
  Object original=method(RegisteredSaveOracle.frameType,"A",RegisteredSaveOracle.xml,RegisteredSaveOracle.sequenceType).invoke(frame,sequence);
  Object packet=original.getClass().getMethod("clone").invoke(original);
  method(RegisteredSaveOracle.xml,"A",void.class,String.class,String.class).invoke(packet,RegisteredSaveOracle.CRS,"MaskGroupBasedCorrections");
  return packet;
 }
 static String serialize(Object packet)throws Exception {return (String)RegisteredSaveOracle.serialize.invoke(null,packet,RegisteredSaveOracle.options.getConstructor(int.class).newInstance(16));}
 public static void main(String[] args)throws Exception {
  RegisteredSaveOracle.initialize();
  StringBuilder out=new StringBuilder("{\"factory_completed\":true,\"cases\":[\n");String comma="";
  for(String filename:new String[]{"frame.nef","frame.jpg","frame.jpeg","frame.png","frame.tif","frame.tiff","frame.dng","frame.cr2","frame.JPG","frame.TIF","frame.DNG","frame","frame.jpg.nef","frame.nef.jpg"})for(String quality:new String[]{null,"","RAW","FLAT","flat","FLAT "}) {
   String saved=serialize(defaults(filename,quality));
   out.append(comma).append("{\"filename\":").append(quote(filename)).append(",\"quality\":").append(quality==null?"null":quote(quality)).append(",\"packet\":").append(quote(saved)).append('}');comma=",\n";
  }
  out.append("\n],\"merges\":[\n");comma="";
  for(String filename:new String[]{"frame.nef","frame.jpg","frame.png"})for(String quality:new String[]{"RAW","FLAT"})for(String property:new String[]{"crs:Exposure2012","crs:Sharpness","crs:LensProfileName","crs:ToneCurvePV2012","crs:WhiteBalance","crs:Look","tiff:Orientation","xmp:Rating","crs:RawFileName"})for(int shape=0;shape<8;shape++) {
   String attrs="lrt:Quality=\""+quality+"\" f:Keep=\"root\"",body="";
   if(shape==0)attrs+=" "+property+"=\".75\"";
   if(shape==1)attrs+=" "+property+"=\"\"";
   if(shape==2)body="<"+property+"> </"+property+">";
   if(shape==3)body="<"+property+" xml:lang=\"en-US\">.25</"+property+">";
   if(shape==4)body="<"+property+" rdf:resource=\"input\"/>";
   if(shape==5)body="<"+property+" rdf:parseType=\"Resource\"><f:Value>input</f:Value><f:Empty/></"+property+">";
   if(shape==6)body="<"+property+"><rdf:Seq><rdf:li>0, 0</rdf:li><rdf:li>255, 240</rdf:li></rdf:Seq></"+property+">";
   if(shape==7)body="<"+property+">custom</"+property+">";
   String input=RegisteredSaveOracle.packet(attrs,body);Object source=RegisteredSaveOracle.parse.invoke(null,input),packet=defaults(filename,quality);
   method(Class.forName("ieA"),"A",void.class,RegisteredSaveOracle.xml,RegisteredSaveOracle.xml,boolean.class,boolean.class,boolean.class).invoke(null,source,packet,true,true,false);
   out.append(comma).append("{\"filename\":").append(quote(filename)).append(",\"quality\":").append(quote(quality)).append(",\"input\":").append(quote(input)).append(",\"packet\":").append(quote(serialize(packet))).append('}');comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
