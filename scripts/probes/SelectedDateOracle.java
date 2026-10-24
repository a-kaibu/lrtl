package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.time.format.DateTimeFormatter;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer: load the target's actual CreateDate cache, copy only
// registered selected cells, and observe the genuine pre-history save prefix.
// The public formatter receives a literal for reproducibility. No native method,
// application singleton, licensing object, or proprietary code is replaced.
public final class SelectedDateOracle {
 public static void main(String[] args)throws Exception {
  RegisteredSaveOracle.initialize();
  int dateIndex=RegisteredSaveOracle.frameType.getField("Xa").getInt(null);
  if(!RegisteredSaveOracle.names.get(dateIndex).equals("xmp:CreateDate"))throw new IllegalStateException("date cache registry changed");
  String marker="clock:synthetic-marker",comma="";
  StringBuilder out=new StringBuilder("{\"store_completed\":false,\"clock_marker\":").append(quote(marker)).append(",\"cases\":[\n");
  for(String date:new String[]{null,"","n/a","2031-02-03T04:05:06.123+09:00","qualified"})
  for(String active:new String[]{"False","True"})for(String sourceActive:new String[]{"False","True"})
  for(String selection:new String[]{"Exposure2012","HasSettings","HasSettings,Exposure2012","WhiteBalance"})
  for(String previous:new String[]{"absent","scalar","qualified","struct"})for(String quality:new String[]{"RAW","FLAT"}) {
   String body="<exif:DateTimeOriginal>2030-original</exif:DateTimeOriginal><xmp:ModifyDate>2032-modified</xmp:ModifyDate><ps:DateCreated xmlns:ps=\"http://ns.adobe.com/photoshop/1.0/\">2033-created</ps:DateCreated>";
   if(date!=null)body+="<xmp:CreateDate"+(date.equals("qualified")?" xml:lang=\"en-US\"":"")+">"+(date.equals("qualified")?"2031-qualified":date)+"</xmp:CreateDate>";
   if(previous.equals("scalar"))body+="<xmp:MetadataDate>previous</xmp:MetadataDate>";
   if(previous.equals("qualified"))body+="<xmp:MetadataDate xml:lang=\"en-US\">previous</xmp:MetadataDate>";
   if(previous.equals("struct"))body+="<xmp:MetadataDate rdf:parseType=\"Resource\"><f:Value>previous</f:Value></xmp:MetadataDate>";
   String target=RegisteredSaveOracle.packet("lrt:Quality=\""+quality+"\" crs:HasSettings=\""+active+"\" crs:Exposure2012=\".25\" crs:WhiteBalance=\"As Shot\"",body);
   String source=RegisteredSaveOracle.packet("lrt:Quality=\"RAW\" crs:HasSettings=\""+sourceActive+"\" crs:Exposure2012=\".75\" crs:WhiteBalance=\"Custom\" xmp:CreateDate=\"2001-source\" exif:DateTimeOriginal=\"2000-source\"","");
   Object packet=RegisteredSaveOracle.parse.invoke(null,target),f=RegisteredSaveOracle.frame(packet),s=RegisteredSaveOracle.frame(RegisteredSaveOracle.parse.invoke(null,source));
   // The generic registered-column loader intentionally skips string slots
   // <=5. Seed this special cache through its genuine public setter using the
   // SDK's own root property reader, as a loaded frame's cache input.
   method(RegisteredSaveOracle.frameType,"h",void.class,String.class).invoke(f,(String)RegisteredSaveOracle.read.invoke(packet,RegisteredSaveOracle.XMP,"CreateDate"));
   String cached=(String)method(RegisteredSaveOracle.frameType,"C",String.class).invoke(f);
   for(String selected:selection.split(",")) {int index=RegisteredSaveOracle.names.indexOf(selected);if(index<5)throw new IllegalStateException(selected);RegisteredSaveOracle.assign.invoke(f,index,RegisteredSaveOracle.cell.invoke(s,index));}
   String after=(String)method(RegisteredSaveOracle.frameType,"C",String.class).invoke(f);
   if(!java.util.Objects.equals(cached,after))throw new IllegalStateException("selected copy changed date cache");
   RegisteredSaveOracle.frameType.getField("ca").set(f,DateTimeFormatter.ofPattern("'"+marker+"'"));
   Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);
   String status="",error=null;
   try{RegisteredSaveOracle.store.invoke(f,sequence,RegisteredSaveOracle.names,false);throw new IllegalStateException("unexpected completed save");}
   catch(InvocationTargetException failure){if(failure.getCause().toString().contains("LRTimelapseMain.m()"))status="history";else{status="error";error=failure.getCause().toString();}}
   method(Class.forName("xOa"),"A",void.class,RegisteredSaveOracle.xml,String.class).invoke(null,packet,"http://ns.adobe.com/xap/1.0/mm/");
   String saved=(String)RegisteredSaveOracle.serialize.invoke(null,packet,RegisteredSaveOracle.options.getConstructor(int.class).newInstance(16));
   out.append(comma).append("{\"target\":").append(quote(target)).append(",\"source\":").append(quote(source)).append(",\"properties\":").append(quote(selection)).append(",\"cache\":").append(cached==null?"null":quote(cached)).append(",\"status\":").append(quote(status)).append(",\"error\":").append(error==null?"null":quote(error)).append(",\"saved\":").append(quote(saved)).append('}');comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
