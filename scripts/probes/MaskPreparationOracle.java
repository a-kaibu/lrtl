package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original synthetic observer of the genuine, completed modern/legacy mask
// preparation helpers. No main, controller, licensing state or method is replaced.
public final class MaskPreparationOracle {
 static String item(int i,String y,String masks) {
  return "<rdf:li rdf:parseType=\"Resource\"><crs:CorrectionName>user-"+i+"</crs:CorrectionName><crs:CorrectionSyncID>input-id-"+i+"</crs:CorrectionSyncID><crs:LocalExposure2012>"+(i+".25")+"</crs:LocalExposure2012><f:Keep>item-"+i+"</f:Keep>"+(masks==null?"":"<crs:CorrectionMasks><rdf:Seq>"+(masks.equals("empty")?"":"<rdf:li rdf:parseType=\"Resource\"><crs:What>Mask/Gradient</crs:What><crs:MaskName>custom</crs:MaskName><crs:MaskValue>.5</crs:MaskValue><crs:ZeroY>"+y+"</crs:ZeroY><f:Keep>mask-"+i+"</f:Keep></rdf:li>"+(masks.equals("two")?"<rdf:li rdf:parseType=\"Resource\"><crs:What>Mask/Gradient</crs:What><crs:MaskName>second</crs:MaskName><crs:ZeroY>.8</crs:ZeroY></rdf:li>":""))+"</rdf:Seq></crs:CorrectionMasks>")+"</rdf:li>";
 }
 static String array(String collection,String kind,String body) {return "<crs:"+collection+"><rdf:"+kind+">"+body+"</rdf:"+kind+"></crs:"+collection+">";}
 static Object prepare(int mode,Object frame,Object sequence,Object packet)throws Exception {
  Method modern=method(RegisteredSaveOracle.frameType,"A",int.class,RegisteredSaveOracle.xml,int.class,RegisteredSaveOracle.sequenceType);
  Method legacy=method(RegisteredSaveOracle.frameType,"a",int.class,RegisteredSaveOracle.xml,int.class,RegisteredSaveOracle.sequenceType);
  if(mode==1){legacy.invoke(frame,packet,0,sequence);return packet;}
  if(mode==3) {
   Method count=method(RegisteredSaveOracle.xml,"A",int.class,String.class,String.class);
   boolean hasLegacy=(int)count.invoke(packet,RegisteredSaveOracle.CRS,"GradientBasedCorrections")>0 || (int)count.invoke(packet,RegisteredSaveOracle.CRS,"CircularGradientBasedCorrections")>0;
   if(hasLegacy){legacy.invoke(frame,packet,0,sequence);packet=method(RegisteredSaveOracle.frameType,"A",RegisteredSaveOracle.xml,RegisteredSaveOracle.xml).invoke(frame,packet);}
  }
  modern.invoke(frame,packet,0,sequence);return packet;
 }
 public static void main(String[] args)throws Exception {
  RegisteredSaveOracle.initialize();
  StringBuilder out=new StringBuilder("{\"helpers_completed\":true,\"cases\":[\n");String comma="";
  for(int mode:new int[]{1,2,3}) {
   String collection=mode==2?"MaskGroupBasedCorrections":"GradientBasedCorrections";
   ArrayList<String[]> cases=new ArrayList<>();cases.add(new String[]{"empty",""});
   for(String kind:new String[]{"Seq","Bag","Alt"})for(int count=0;count<=12;count++)for(String masks:new String[]{"one","empty",null}) {
    StringBuilder items=new StringBuilder();for(int i=1;i<=count;i++)items.append(item(i,".7",masks));
    cases.add(new String[]{"count-"+kind+"-"+count+"-"+masks,array(collection,kind,items.toString())});
   }
   for(int position=1;position<=12;position++)for(String y:new String[]{"-0.41","-0.31","-0.21"," -0.41","-0.4","-0.399","-0.2","-0.4105"}) {
    StringBuilder items=new StringBuilder();for(int i=1;i<=12;i++)items.append(item(i,i==position?y:".7","two"));
    cases.add(new String[]{"role-"+position+"-"+y,array(collection,"Seq",items.toString())});
   }
   for(int count=0;count<=9;count++) {
    StringBuilder items=new StringBuilder();for(int i=1;i<=count;i++)items.append(item(i,".7","one"));
    cases.add(new String[]{"other-arrays-"+count,array(mode==2?"GradientBasedCorrections":"MaskGroupBasedCorrections","Seq",items.toString())+array("CircularGradientBasedCorrections","Seq",item(1,".3",null))+array("PaintBasedCorrections","Seq",items.toString())});
   }
   for(String body:new String[]{"<crs:"+collection+">scalar</crs:"+collection+">",array(collection,"Seq","<rdf:li>scalar</rdf:li>"),array(collection,"Seq","<rdf:li rdf:resource=\"urn:synthetic\"/>"),array(collection,"Seq","<rdf:li rdf:parseType=\"Resource\"><crs:CorrectionMasks>scalar</crs:CorrectionMasks></rdf:li>")})cases.add(new String[]{"invalid-"+cases.size(),body});
   for(String[] c:cases) {
    String text=RegisteredSaveOracle.packet("f:Keep=\"root\"",c[1]);
    Object packet=RegisteredSaveOracle.parse.invoke(null,text);
    Object frame=RegisteredSaveOracle.frameType.getConstructor(Object[].class).newInstance((Object)new Object[RegisteredSaveOracle.names.size()]);
    Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,mode);
    String status="ok",error=null;
    try{packet=prepare(mode,frame,sequence,packet);}catch(InvocationTargetException failure){status="error";error=failure.getCause().toString();}
    String saved=(String)RegisteredSaveOracle.serialize.invoke(null,packet,RegisteredSaveOracle.options.getConstructor(int.class).newInstance(16));
    String repeated=null;
    if(status.equals("ok")){packet=prepare(mode,frame,sequence,packet);repeated=(String)RegisteredSaveOracle.serialize.invoke(null,packet,RegisteredSaveOracle.options.getConstructor(int.class).newInstance(16));}
    out.append(comma).append("{\"mode\":").append(mode).append(",\"name\":").append(quote(c[0])).append(",\"input\":").append(quote(text)).append(",\"status\":").append(quote(status)).append(",\"error\":").append(error==null?"null":quote(error)).append(",\"saved\":").append(quote(saved)).append(",\"repeated\":").append(repeated==null?"null":quote(repeated)).append('}');comma=",\n";
   }
  }
  Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
 }
}
