package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.time.*;
import java.time.format.DateTimeFormatter;
import java.util.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of the default date formatter and genuine save prefix.
// For deterministic branch observations only, the frame's public formatter
// configuration is a literal marker. Default formatting is measured separately
// without that configuration change. No method/main/license object is replaced.
public final class MetadataDateOracle {
 static final String MARKER="clock:synthetic-marker";
 public static void main(String[] args)throws Exception {
  RegisteredSaveOracle.initialize();
  Object sample=RegisteredSaveOracle.frame(RegisteredSaveOracle.parse.invoke(null,RegisteredSaveOracle.packet("lrt:Quality=\"RAW\" crs:HasSettings=\"True\"","")));
  Field formatter=RegisteredSaveOracle.frameType.getField("ca");
  DateTimeFormatter normal=(DateTimeFormatter)formatter.get(sample);
  if(args.length==2&&args[1].equals("--live")) {
   Object packet=method(RegisteredSaveOracle.frameType,"A",RegisteredSaveOracle.xml).invoke(sample);
   method(RegisteredSaveOracle.frameType,"h",void.class,String.class).invoke(sample,"2026-10-24T03:04:05.12");
   Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);
   Instant before=Instant.now();String saved=RegisteredSaveOracle.save(sample,packet,sequence);Instant after=Instant.now();
   Object result=RegisteredSaveOracle.parse.invoke(null,saved);
   String clock=(String)RegisteredSaveOracle.read.invoke(result,"http://ns.adobe.com/xap/1.0/","MetadataDate");
   Files.writeString(Path.of(args[0]),"{\"before\":"+quote(before.toString())+",\"after\":"+quote(after.toString())+",\"clock\":"+quote(clock)+"}\n");return;
  }
  StringBuilder out=new StringBuilder("{\"store_completed\":false,\"stop\":\"history: null LRTimelapseMain.m()\",\"clock_marker\":").append(quote(MARKER)).append(",\"default_formatter\":").append(quote(normal.toString())).append(",\"formats\":[\n");String comma="";
  String[] instants={"1970-01-01T00:00:00Z","2026-10-24T03:04:05.123456789Z","2026-10-24T03:04:05.000000001Z","2026-10-24T03:04:05.999999999Z","2000-02-29T23:59:59Z","2021-01-01T00:00:00Z","0000-01-01T00:00:00Z","9999-12-31T23:59:59.999999999Z"};
  String[] zones={"UTC","+09:00","-03:30","+05:45","+00:00:30","-00:44:30","Europe/Berlin","America/New_York"};
  for(String instant:instants)for(String zone:zones) {
   ZonedDateTime date=Instant.parse(instant).atZone(ZoneId.of(zone));
   out.append(comma).append("{\"instant\":").append(quote(instant)).append(",\"zone\":").append(quote(zone)).append(",\"offset_seconds\":").append(date.getOffset().getTotalSeconds()).append(",\"expected\":").append(quote(normal.format(date))).append('}');comma=",\n";
  }
  out.append("\n],\"cases\":[\n");comma="";
  for(String quality:new String[]{null,"","RAW","FLAT"})for(String active:new String[]{null,"","False","True","TRUE","true","1"," true "})for(String date:new String[]{null,"","n/a","2026-10-24T03:04:05.12"})for(String previous:new String[]{"absent","scalar","qualified","struct"}) {
   String attrs=(quality==null?"":"lrt:Quality=\""+quality+"\" ")+(active==null?"":"crs:HasSettings=\""+active+"\"");
   String body="";
   if(previous.equals("scalar"))body="<xmp:MetadataDate>previous</xmp:MetadataDate>";
   if(previous.equals("qualified"))body="<xmp:MetadataDate xml:lang=\"en-US\">previous</xmp:MetadataDate>";
   if(previous.equals("struct"))body="<xmp:MetadataDate rdf:parseType=\"Resource\"><f:Value>previous</f:Value></xmp:MetadataDate>";
   String target=RegisteredSaveOracle.packet(attrs,body);
   Object packet=RegisteredSaveOracle.parse.invoke(null,target),frame=RegisteredSaveOracle.frame(packet);
   formatter.set(frame,DateTimeFormatter.ofPattern("'"+MARKER+"'"));
   method(RegisteredSaveOracle.frameType,"h",void.class,String.class).invoke(frame,date);
   Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);
   String status="",error=null;
   try {RegisteredSaveOracle.store.invoke(frame,sequence,RegisteredSaveOracle.names,false);throw new IllegalStateException("unexpected completed native save");}
   catch(InvocationTargetException failure) {
    if(failure.getCause().toString().contains("LRTimelapseMain.m()"))status="history";
    else {status="error";error=failure.getCause().toString();}
   }
   method(Class.forName("xOa"),"A",void.class,RegisteredSaveOracle.xml,String.class).invoke(null,packet,"http://ns.adobe.com/xap/1.0/mm/");
   String saved=(String)RegisteredSaveOracle.serialize.invoke(null,packet,RegisteredSaveOracle.options.getConstructor(int.class).newInstance(16));
   out.append(comma).append("{\"quality\":").append(quality==null?"null":quote(quality)).append(",\"active\":").append(active==null?"null":quote(active)).append(",\"date\":").append(date==null?"null":quote(date)).append(",\"previous\":").append(quote(previous)).append(",\"status\":").append(quote(status)).append(",\"error\":").append(error==null?"null":quote(error)).append(",\"target\":").append(quote(target)).append(",\"saved\":").append(quote(saved)).append('}');comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
