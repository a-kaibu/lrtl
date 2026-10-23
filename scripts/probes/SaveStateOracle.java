package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;

import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original synthetic observer for WB cleanup, cached crop and the save prefix.
// The store stops at its missing GUI history dependency; no controller or
// licensing state is stubbed.
public final class SaveStateOracle {
 static final String CRS="http://ns.adobe.com/camera-raw-settings/1.0/";
 static final String[] NUMBERS={"CropTop","CropLeft","CropBottom","CropRight","CropAngle","Temperature","Tint","IncrementalTemperature","IncrementalTint"};
 static Class<?> frameType,xmlType,optionsType;
 static Method parsePacket,serializePacket,loadCell,indexOf,baselineCells,qualityOf,store;
 static int columnCount;
 static Object frame(String text,String quality)throws Exception {
  Object frame=frameType.getConstructor(Object[].class).newInstance((Object)new Object[columnCount]);
  baselineCells.invoke(frame,(Object)new Object[columnCount]);qualityOf.invoke(frame,quality);
  Object packet=parsePacket.invoke(null,text);
  List<String> names=new ArrayList<>(Arrays.asList(NUMBERS));names.add("WhiteBalance");names.add("HasCrop");
  for(String name:names)loadCell.invoke(frame,name,CRS,packet,(int)indexOf.invoke(null,name),false);
  return frame;
 }
 static String savedPrefix(Object frame,Object packet)throws Exception {
  Field attached=frameType.getDeclaredField("Aa");attached.setAccessible(true);attached.set(frame,packet);
  List<String> names=new ArrayList<>(Arrays.asList(NUMBERS));names.add("WhiteBalance");names.add("HasCrop");
  int last=0;for(String name:names)last=Math.max(last,(int)indexOf.invoke(null,name));
  ArrayList<String> fields=new ArrayList<>(Collections.nCopies(last+1,"unused"));for(String name:names)fields.set((int)indexOf.invoke(null,name),name);
  Object sequence=Class.forName("Rsa").getConstructor(int.class).newInstance(-1);String error="";
  try{store.invoke(frame,sequence,fields,false);}catch(InvocationTargetException failure){error=failure.getCause().toString();}
  if(!error.contains("LRTimelapseMain.m()"))throw new IllegalStateException("Unexpected history-stage result: "+error);
  method(Class.forName("xOa"),"A",void.class,xmlType,String.class).invoke(null,packet,"http://ns.adobe.com/xap/1.0/mm/");
  return (String)serializePacket.invoke(null,packet,optionsType.getConstructor(int.class).newInstance(16));
 }
 static String escape(String s){return s.replace("&","&amp;").replace("\"","&quot;").replace("<","&lt;");}
 static String packet(Map<String,String> values,int layout){
  StringBuilder attrs=new StringBuilder(),body=new StringBuilder();
  for(var entry:values.entrySet())if(entry.getValue()!=null){
   String tag=entry.getKey();
   if(layout==0)attrs.append(' ').append(tag).append("=\"").append(escape(entry.getValue())).append('"');
   else body.append('<').append(tag).append('>').append(escape(entry.getValue())).append("</").append(tag).append('>');
  }
  String head="<rdf:Description rdf:about=\"\" xmlns:crs=\""+CRS+"\" xmlns:lrt=\"http://lrtimelapse.com/\" xmlns:f=\"urn:lrtl:save-state/\"";
  String contents=head+attrs+">"+body+"<f:Nested rdf:parseType=\"Resource\"><crs:Temperature>777</crs:Temperature></f:Nested></rdf:Description>";
  if(layout==2)contents=head+" f:Keep=\"untouched\"/>"+contents;
  return "<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">"+contents+"</rdf:RDF></x:xmpmeta>";
 }
 public static void main(String[] args)throws Exception{
  Class<?> f=Class.forName("tQa"),xml=Class.forName("Wd"),factory=Class.forName("ABA"),options=Class.forName("bCA"),reg=Class.forName("Ooa");
  Object registry=method(reg,"A",reg).invoke(null);int count=(int)method(reg,"A",int.class).invoke(registry);
  Method parse=method(factory,"A",xml,String.class),serialize=method(factory,"A",String.class,xml,options);
  Method cleanup=method(f,"h",void.class,xml),quality=method(f,"K",void.class,String.class);
  Method baseline=method(f,"A",void.class,Object[].class),crop=method(f,"j",boolean.class);
  Method index=method(f,"A",int.class,String.class),load=method(f,"A",void.class,String.class,String.class,xml,int.class,boolean.class);
  Method get=method(f,"a",Object.class,int.class),set=method(f,"a",void.class,int.class,Object.class);
  frameType=f;xmlType=xml;optionsType=options;columnCount=count;parsePacket=parse;serializePacket=serialize;loadCell=load;indexOf=index;baselineCells=baseline;qualityOf=quality;store=method(f,"A",void.class,Class.forName("Rsa"),ArrayList.class,boolean.class);
  if(args.length==2&&args[1].equals("--scope")){
   String source="<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description xmlns:crs=\""+CRS+"\" xmlns:f=\"urn:lrtl:save-state/\" crs:WhiteBalance=\"As Shot\" crs:Temperature=\"4500\" crs:CropLeft=\".25\" crs:HasCrop=\"True\"><f:Nested><rdf:Description crs:Temperature=\"777\" crs:CropRight=\".5\"/></f:Nested></rdf:Description></rdf:RDF></x:xmpmeta>";
   Object document=parse.invoke(null,source);cleanup.invoke(frame(source,null),document);
   Method read=method(xml,"A",String.class,String.class,String.class);
   String temperature=(String)read.invoke(document,CRS,"Temperature"),right=(String)read.invoke(document,CRS,"CropRight");
   String nestedTemperature=(String)read.invoke(document,"urn:lrtl:save-state/","Nested/crs:Temperature"),nestedRight=(String)read.invoke(document,"urn:lrtl:save-state/","Nested/crs:CropRight");
   String expected=(String)serialize.invoke(null,document,options.getConstructor(int.class).newInstance(16));
   Files.writeString(Path.of(args[0]),"{\"source\":"+quote(source)+",\"expected\":"+quote(expected)+",\"temperature\":"+(temperature==null?"null":quote(temperature))+",\"right\":"+(right==null?"null":quote(right))+",\"nested_temperature\":"+quote(nestedTemperature)+",\"nested_right\":"+quote(nestedRight)+"}\n");return;
  }
  StringBuilder out=new StringBuilder("{\"store_completed\":false,\"store_stop\":\"history: null LRTimelapseMain.m()\",\"wb\":[\n");int wbCount=0;
  for(String q:new String[]{null,"","RAW","FLAT","flat","JPEG","DNG","RAW+JPEG"})for(String wb:new String[]{null,"As Shot","as shot","AS SHOT","Custom","custom",""," ","Daylight","日本語"," As Shot "})for(int layout=0;layout<3;layout++){
   Map<String,String> values=new LinkedHashMap<>();
   values.put("lrt:Quality",q);values.put("crs:WhiteBalance",wb);
   values.put("crs:Temperature","5500");values.put("crs:Tint","20");values.put("crs:IncrementalTemperature",".25");values.put("crs:IncrementalTint","-.1");values.put("crs:Contrast2012","17");
   String source=packet(values,layout);Object document=parse.invoke(null,source),frame=f.getConstructor(Object[].class).newInstance((Object)new Object[count]);
   baseline.invoke(frame,(Object)new Object[count]);quality.invoke(frame,q);cleanup.invoke(frame,document);
   String expected=(String)serialize.invoke(null,document,options.getConstructor(int.class).newInstance(16));
   String saved=savedPrefix(frame(source,q),parse.invoke(null,source));
   if(wbCount++>0)out.append(",\n");out.append("{\"quality\":").append(q==null?"null":quote(q)).append(",\"source\":").append(quote(source)).append(",\"expected\":").append(quote(expected)).append(",\"saved\":").append(quote(saved)).append('}');
  }
  for(String[] pair:new String[][]{{"RAW","FLAT"},{"FLAT","RAW"},{null,"FLAT"}}){
   String q=pair[0];Map<String,String> values=new LinkedHashMap<>();values.put("lrt:Quality",pair[1]);values.put("crs:WhiteBalance","Custom");values.put("crs:Temperature","5500");values.put("crs:Tint","20");values.put("crs:IncrementalTemperature",".25");values.put("crs:IncrementalTint","-.1");values.put("crs:Contrast2012","17");
   String source=packet(values,0);Object document=parse.invoke(null,source),frame=f.getConstructor(Object[].class).newInstance((Object)new Object[count]);baseline.invoke(frame,(Object)new Object[count]);quality.invoke(frame,q);cleanup.invoke(frame,document);
   String expected=(String)serialize.invoke(null,document,options.getConstructor(int.class).newInstance(16)),saved=savedPrefix(frame(source,q),parse.invoke(null,source));
   if(wbCount++>0)out.append(",\n");out.append("{\"quality\":").append(q==null?"null":quote(q)).append(",\"source\":").append(quote(source)).append(",\"expected\":").append(quote(expected)).append(",\"saved\":").append(quote(saved)).append('}');
  }
  String[] properties={"CropTop","CropLeft","CropBottom","CropRight","CropAngle"};
  out.append("\n],\"crop\":[\n");int cropCount=0;
  List<Map<String,String>> crops=new ArrayList<>();crops.add(new LinkedHashMap<>());
  for(String name:properties)for(String value:new String[]{null,"0","-0.0","1",".25","0.0000004","NaN","Infinity","-Infinity","bad","0x1p-2","+ 0.25"}){
   Map<String,String> values=new LinkedHashMap<>();values.put("crs:"+name,value);crops.add(values);
  }
  Map<String,String> defaults=new LinkedHashMap<>();for(int i=0;i<properties.length;i++)defaults.put("crs:"+properties[i],i==2||i==3?"1":"0");crops.add(defaults);
  for(Map<String,String> input:crops)for(String flag:new String[]{"True","False"}){
   Map<String,String> values=new LinkedHashMap<>(input);values.put("crs:HasCrop",flag);
   String source=packet(values,0);Object document=parse.invoke(null,source),frame=f.getConstructor(Object[].class).newInstance((Object)new Object[count]);
   for(String name:properties){int col=(int)index.invoke(null,name);load.invoke(frame,name,CRS,document,col,false);}
   boolean active=(boolean)crop.invoke(frame);
   String saved=savedPrefix(frame(source,null),parse.invoke(null,source));
   if(cropCount++>0)out.append(",\n");out.append("{\"source\":").append(quote(source)).append(",\"expected_active\":").append(active).append(",\"saved\":").append(quote(saved)).append('}');
  }
  out.append("\n],\"copy_crop\":[\n");
  String[][] selections={{"CropLeft"},{"CropLeft"},{"CropRight"},{"HasCrop"},{"CropAngle"},{"CropLeft","CropAngle"}};
  String[][] sourceValues={{"CropLeft","0.0000004"},{"CropLeft",null},{"CropRight",null},{"HasCrop","False"},{"CropAngle","NaN"},{"CropLeft","0","CropAngle","0"}};
  for(int i=0;i<selections.length;i++){
   Map<String,String> targetValues=new LinkedHashMap<>(defaults);if(i==1||i==3||i==5)targetValues.put("crs:CropLeft",".2");if(i==2)targetValues.put("crs:CropRight",".8");
   Map<String,String> srcValues=new LinkedHashMap<>();for(int j=0;j<sourceValues[i].length;j+=2)srcValues.put("crs:"+sourceValues[i][j],sourceValues[i][j+1]);
   String targetText=packet(targetValues,0),sourceText=packet(srcValues,0);
   Object target=parse.invoke(null,targetText),source=parse.invoke(null,sourceText),destFrame=frame(targetText,null),srcFrame=frame(sourceText,null);
   for(String name:selections[i]){int col=(int)index.invoke(null,name);if(name.equals("HasCrop"))load.invoke(srcFrame,name,CRS,source,col,false);set.invoke(destFrame,col,get.invoke(srcFrame,col));}
   boolean active=(boolean)crop.invoke(destFrame);String saved=savedPrefix(destFrame,target);
   if(i>0)out.append(",\n");out.append("{\"source\":").append(quote(sourceText)).append(",\"destination\":").append(quote(targetText)).append(",\"properties\":").append(quote(String.join(",",selections[i]))).append(",\"expected_active\":").append(active).append(",\"saved\":").append(quote(saved)).append('}');
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
  System.out.println("OBSERVED "+wbCount+" WB, "+cropCount+" CROP AND "+selections.length+" CACHED COPY CASES");
 }
}
