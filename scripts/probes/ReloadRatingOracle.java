package lrtlprobe;
import java.io.File;
import java.lang.reflect.*;
import java.nio.file.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of genuine default preparation followed by the actual
// frame cache loader. The latter stops at absent main-controller settings only
// after assigning the rating and date caches. No native method/main/license
// object is replaced, and no user files or preferences are written.
public final class ReloadRatingOracle {
 public static void main(String[] args)throws Exception {
  RegisteredSaveOracle.initialize();
  StringBuilder out=new StringBuilder("{\"reload_completed\":false,\"stop\":\"null LRTimelapseMain.N.s after rating/date cache assignment\",\"cases\":[\n");String comma="";
  for(String parent:new String[]{"sequence",".LRT",".LRT-proxy",".lrt",".lrt-proxy","prefix.LRT",".Lrt","LRT","LRT test",".LRT_","LRT_","_LRT",".LR","LRT_export","LRT__","lrt_","Lrt_","prefixLRT_","LRT_export/inner","outer/LRT_export"})
  for(String value:new String[]{null,"","0","1","2","3","4","5","-1","6","+1","+2","+3","+4","++1","+-1","--1"," 1","1 "," 4 ","\t4","4\n","4.0","1.5","NaN","Infinity","2147483647","2147483648","-2147483648","-2147483649","４","２","٣","𝟜","+ 1"," +1","--2","２ ","２.0","-0","+0","1e0","true"})for(String form:new String[]{"attribute","qualified","uri","wrapped"}) {
   String attrs="lrt:Quality=\"RAW\" xmp:CreateDate=\"2031-01-01T00:00:00\" f:Keep=\"root\"",body="";
   if(value!=null) {
    String escaped=value.replace("&","&amp;").replace("\"","&quot;").replace("\t","&#9;").replace("\n","&#10;");
    if(form.equals("attribute"))attrs+=" xmp:Rating=\""+escaped+"\"";
    if(form.equals("qualified"))body="<xmp:Rating xml:lang=\"en-US\">"+escaped+"</xmp:Rating>";
    if(form.equals("wrapped"))body="<xmp:Rating rdf:parseType=\"Resource\"><rdf:value>"+escaped+"</rdf:value><f:Qualifier>keep</f:Qualifier></xmp:Rating>";
    if(form.equals("uri"))body="<xmp:Rating rdf:resource=\""+escaped+"\"/>";
   }
   String input=RegisteredSaveOracle.packet(attrs,body);
   Object packet=RegisteredSaveOracle.parse.invoke(null,input),frame=RegisteredSaveOracle.frameType.getConstructor(Object[].class).newInstance((Object)new Object[RegisteredSaveOracle.names.size()]);
   File image=new File(new File(System.getProperty("java.io.tmpdir"),parent),"frame.nef");
   method(RegisteredSaveOracle.frameType,"a",void.class,File.class).invoke(frame,image);
   RegisteredSaveOracle.assign.invoke(frame,RegisteredSaveOracle.frameType.getField("aa").getInt(null),image.getName());
   method(RegisteredSaveOracle.frameType,"K",void.class,String.class).invoke(frame,"RAW");
   Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);
   packet=method(RegisteredSaveOracle.frameType,"A",RegisteredSaveOracle.xml,RegisteredSaveOracle.sequenceType,RegisteredSaveOracle.xml).invoke(frame,sequence,packet);
   method(RegisteredSaveOracle.frameType,"a",void.class,RegisteredSaveOracle.xml).invoke(frame,packet);
   try{method(RegisteredSaveOracle.frameType,"A",void.class,RegisteredSaveOracle.sequenceType,boolean.class,boolean.class).invoke(frame,sequence,false,false);throw new IllegalStateException("unexpected complete reload");}
   catch(InvocationTargetException failure){String error=failure.getCause().toString();if(!error.contains("LRTimelapseMain.N")||!error.contains("\"s\""))throw failure;}
   int rating=(int)method(RegisteredSaveOracle.frameType,"C",int.class).invoke(frame);
   String loaded=(String)RegisteredSaveOracle.read.invoke(packet,RegisteredSaveOracle.XMP,"Rating");
   out.append(comma).append("{\"parent\":").append(quote(parent)).append(",\"value\":").append(value==null?"null":quote(value)).append(",\"form\":").append(quote(form)).append(",\"input\":").append(quote(input)).append(",\"after_defaults\":").append(loaded==null?"null":quote(loaded)).append(",\"expected\":").append(rating).append('}');comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
