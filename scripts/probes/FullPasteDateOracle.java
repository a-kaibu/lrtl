package lrtlprobe;

import java.io.File;
import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of the genuine full-paste caller through its reload's date
// cache assignment. A constructor-free controller owns a genuine table model;
// no global main/controller, license object, or product method is substituted.
// Reload stops at the absent main controller, after its date/cache work.
public final class FullPasteDateOracle {
 static String date(String name,String value) {
  if(value==null)return "";
  if(value.equals("qualified"))return "<"+name+" xml:lang=\"en-US\">2001-qualified</"+name+">";
  return "<"+name+">"+value+"</"+name+">";
 }
 public static void main(String[] args)throws Exception {
  RegisteredSaveOracle.initialize();
  Class<?> controller=Class.forName("lrtimelapse.cOM7"),unsafe=Class.forName("sun.misc.Unsafe"),model=Class.forName("lrtimelapse.com8");
  Field one=unsafe.getDeclaredField("theUnsafe");one.setAccessible(true);
  Object shell=unsafe.getMethod("allocateInstance",Class.class).invoke(one.get(null),controller);
  Object table=model.getConstructor(ArrayList.class).newInstance(RegisteredSaveOracle.names);
  Field wc=controller.getDeclaredField("wc");wc.setAccessible(true);wc.set(shell,table);
  Object sequence=method(model,"A",RegisteredSaveOracle.sequenceType).invoke(table);
  method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);
  Method paste=method(controller,"A",void.class,RegisteredSaveOracle.frameType,RegisteredSaveOracle.frameType,boolean.class,boolean.class);
  int activeIndex=RegisteredSaveOracle.names.indexOf("HasSettings");
  if(activeIndex<5)throw new IllegalStateException("missing HasSettings");
  StringBuilder out=new StringBuilder("{\"paste_completed\":false,\"stop\":\"reload: null LRTimelapseMain.N.s after date cache assignment\",\"cases\":[\n");String comma="";
  ArrayList<String[]> inputs=new ArrayList<>();
  String[] targetCreates={null,"","n/a","2031:02:03 04:05:06","2031-02-03T04:05:06.123456+09:00","2031-02-03T04:05:06+09:00","qualified","🙂2031:02:03 04:05:06.123"};
  for(String create:targetCreates)for(String original:new String[]{null,"","2030:02:03 04:05:06","2030-02-03T04:05:06.1234+09:00"})
  for(String sourceCreate:new String[]{null,"","2001-02-03T04:05:06.1234+09:00","qualified"})for(String sourceOriginal:new String[]{null,"2000:02:03 04:05:06"})for(String active:new String[]{null,"","False","True","TRUE","1","0"," true "}) {
   String target=RegisteredSaveOracle.packet("lrt:Quality=\"RAW\" xmp:Rating=\"0\"",date("xmp:CreateDate",create)+date("exif:DateTimeOriginal",original));
   String source=RegisteredSaveOracle.packet((active==null?"":"crs:HasSettings=\""+active+"\" ")+"crs:Exposure2012=\".75\"",date("xmp:CreateDate",sourceCreate)+date("exif:DateTimeOriginal",sourceOriginal));
   inputs.add(new String[]{target,source});
  }
  for(String active:new String[]{"2","-1","+1","01","-0","00","yes","YES","on","ON","t","T","false","yeſ","no","off","f","true "," true"," true ","+0","-2","2147483647","2147483648","-2147483648","-2147483649","１","０","++1","+-1","1.0","NaN"}) {
   inputs.add(new String[]{RegisteredSaveOracle.packet("lrt:Quality=\"RAW\" xmp:Rating=\"0\"",date("xmp:CreateDate","2031-02-03T04:05:06.123456+09:00")),RegisteredSaveOracle.packet("crs:HasSettings=\""+active+"\" crs:Exposure2012=\".75\"","")});
  }
  for(String[] input:inputs) {
   String target=input[0],source=input[1];
   Object f=RegisteredSaveOracle.frame(RegisteredSaveOracle.parse.invoke(null,target)),s=RegisteredSaveOracle.frame(RegisteredSaveOracle.parse.invoke(null,source));
   File image=new File(System.getProperty("java.io.tmpdir"),"lrtl-original-full-paste-observer.nef");
   method(RegisteredSaveOracle.frameType,"a",void.class,File.class).invoke(f,image);
   RegisteredSaveOracle.assign.invoke(f,RegisteredSaveOracle.frameType.getField("aa").getInt(null),image.getName());
   try{paste.invoke(shell,f,s,true,false);throw new IllegalStateException("unexpected completed full paste");}
   catch(InvocationTargetException failure){String error=failure.getCause().toString();if(!error.contains("LRTimelapseMain.N")||!error.contains("\"s\""))throw failure;}
   String cached=(String)method(RegisteredSaveOracle.frameType,"C",String.class).invoke(f);
   Object loadedActive=RegisteredSaveOracle.cell.invoke(f,activeIndex);
   out.append(comma).append("{\"target\":").append(quote(target)).append(",\"source\":").append(quote(source)).append(",\"cache\":").append(cached==null?"null":quote(cached)).append(",\"loaded_has_settings\":").append(loadedActive==null?"null":quote(loadedActive.toString())).append('}');comma=",\n";
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
