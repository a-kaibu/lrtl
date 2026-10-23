package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;

import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original synthetic observer of the registered-column save loop and its
// pre-history metadata effects. No GUI/main or licensing state is replaced.
public final class RegisteredSaveOracle {
    static final String CRS="http://ns.adobe.com/camera-raw-settings/1.0/",LRT="http://lrtimelapse.com/",XMP="http://ns.adobe.com/xap/1.0/";
    static Class<?> frameType,xml,options,sequenceType;
    static Method parse,serialize,load,cell,assign,read,store;
    static ArrayList<String> names=new ArrayList<>();
    static ArrayList<Class<?>> types=new ArrayList<>();
    static StringBuilder out=new StringBuilder("{\"store_completed\":false,\"stop\":\"history: null LRTimelapseMain.m()\",\"cases\":[\n");
    static String separator="";
    static String save(Object f,Object packet,Object sequence)throws Exception {
        String stop="";
        try{store.invoke(f,sequence,names,false);}catch(InvocationTargetException failure){stop=failure.getCause().toString();}
        if(!stop.contains("LRTimelapseMain.m()"))throw new IllegalStateException("Unexpected stop "+stop);
        method(Class.forName("xOa"),"A",void.class,xml,String.class).invoke(null,packet,"http://ns.adobe.com/xap/1.0/mm/");
        return (String)serialize.invoke(null,packet,options.getConstructor(int.class).newInstance(16));
    }
    static void field(Object frame,String name,Object value)throws Exception {Field f=frameType.getDeclaredField(name);f.setAccessible(true);f.set(frame,value);}
    static String packet(String attrs,String body) {return MetadataPasteOracle.packet(attrs,body).replace("xmlns:crs=", "xmlns:crd=\"http://ns.adobe.com/camera-raw-defaults/1.0/\" xmlns:crs=");}
    static Object frame(Object packet)throws Exception {
        Object f=frameType.getConstructor(Object[].class).newInstance((Object)new Object[names.size()]);
        method(frameType,"A",void.class,Object[].class).invoke(f,(Object)new Object[names.size()]);
        String q=(String)read.invoke(packet,LRT,"Quality");method(frameType,"K",void.class,String.class).invoke(f,q);
        for(int i=5;i<names.size();i++)load.invoke(f,names.get(i),CRS,packet,i,false);
        String rating=(String)read.invoke(packet,XMP,"Rating");
        if(rating!=null)method(frameType,"E",void.class,int.class).invoke(f,Integer.parseInt(rating));
        boolean modern=method(xml,"A",boolean.class,String.class,String.class).invoke(packet,CRS,"MaskGroupBasedCorrections").equals(true);
        @SuppressWarnings("unchecked") Map<Integer,Integer> roles=(Map<Integer,Integer>)method(frameType,"A",HashMap.class,xml).invoke(f,packet);
        if(modern)field(f,"I",roles);
        for(int role=1;role<=3;role++) {
            String property=(modern?"MaskGroupBasedCorrections["+roles.get(role):"GradientBasedCorrections["+(role+4))+"]/crs:LocalExposure2012";
            String value=(String)read.invoke(packet,CRS,property);
            double number=value==null?0:Double.parseDouble(value);
            method(frameType,role==1?"L":role==2?"K":"m",void.class,Double.class).invoke(f,number);
        }
        method(frameType,"a",void.class,xml).invoke(f,packet);
        return f;
    }
    static void observe(String name,String targetText,String sourceText,String selection)throws Exception {
        Object packet=parse.invoke(null,targetText),f=frame(packet);
        if(sourceText!=null) {
            Object source=frame(parse.invoke(null,sourceText));
            for(String selected:selection.split(",")) {int index=names.indexOf(selected);if(index<5)throw new IllegalArgumentException(selected);assign.invoke(f,index,cell.invoke(source,index));}
        }
        Object sequence=sequenceType.getConstructor(int.class).newInstance(-1);
        boolean modern=(boolean)method(xml,"A",boolean.class,String.class,String.class).invoke(packet,CRS,"MaskGroupBasedCorrections");
        if(!modern) {
            boolean legacy=false;for(String collection:new String[]{"GradientBasedCorrections","CircularGradientBasedCorrections","PaintBasedCorrections"})legacy|=(boolean)method(xml,"A",boolean.class,String.class,String.class).invoke(packet,CRS,collection);
            modern=!legacy;
        }
        method(sequenceType,"d",void.class,int.class).invoke(sequence,modern?2:1);
        String saved=save(f,packet,sequence),savedAgain=save(f,packet,sequence);
        out.append(separator).append("{\"name\":").append(quote(name)).append(",\"target\":").append(quote(targetText)).append(",\"source\":").append(sourceText==null?"null":quote(sourceText)).append(",\"properties\":").append(selection==null?"null":quote(selection)).append(",\"saved\":").append(quote(saved)).append(",\"saved_again\":").append(quote(savedAgain)).append('}');separator=",\n";
    }
    static void initialize()throws Exception {
        Locale.setDefault(Locale.JAPAN);
        frameType=Class.forName("tQa");xml=Class.forName("Wd");options=Class.forName("bCA");sequenceType=Class.forName("Rsa");Class<?> registryType=Class.forName("Ooa"),factory=Class.forName("ABA");
        Object registry=method(registryType,"A",registryType).invoke(null);int count=(int)method(registryType,"A",int.class).invoke(registry);
        for(int i=0;i<count;i++){names.add((String)method(registryType,"a",String.class,int.class).invoke(registry,i));types.add((Class<?>)method(registryType,"A",Class.class,int.class).invoke(registry,i));}
        parse=method(factory,"A",xml,String.class);serialize=method(factory,"A",String.class,xml,options);read=method(xml,"A",String.class,String.class,String.class);
        load=method(frameType,"A",void.class,String.class,String.class,xml,int.class,boolean.class);cell=method(frameType,"a",Object.class,int.class);assign=method(frameType,"a",void.class,int.class,Object.class);
        store=method(frameType,"A",void.class,sequenceType,ArrayList.class,boolean.class);
    }
    public static void main(String[] args)throws Exception {
        initialize();
        String base="crs:WhiteBalance=\"Custom\" lrt:Quality=\"RAW\" f:Keep=\"untouched\"";
        for(int i=5;i<names.size();i++)if(types.get(i)==Double.class||types.get(i)==Integer.class) {
            String p=names.get(i),value=types.get(i)==Double.class?"1.23456789123":"1.5";
            String target=packet(base,SelectedColumnOracle.pathBody(p,value));
            observe("numeric-"+i,target,null,null);
        }
        String empty=packet("","");
        for(String q:new String[]{null,"","RAW","FLAT"})for(String name:new String[]{"ProcessVersion","CropLeft","HDREditMode","crs:CameraProfile","crd:CameraProfile","MaskGroupBasedCorrections[1]/crs:LocalExposure2012"}) {
            String value=name.contains("Profile")?"target":"0.25";
            String attrs="crs:WhiteBalance=\"As Shot\""+(q==null?"":" lrt:Quality=\""+q+"\"");
            observe("clear-"+q+"-"+name,packet(attrs,SelectedColumnOracle.pathBody(name,value)+"<lrt:ProcessVersion>foreign</lrt:ProcessVersion>"),empty,name);
        }
        for(String active:new String[]{"true","TRUE","false","1",""})for(int rating:new int[]{-1,0,1,3,4,5}) {
            observe("rating-"+active+"-"+rating,packet("crs:HasSettings=\""+active+"\" xmp:Rating=\""+rating+"\"", ""),null,null);
        }
        observe("reference-tags",packet(base,"<lrt:ReferenceKeyframe>4</lrt:ReferenceKeyframe><lrt:ReferenceKeyFrame>5</lrt:ReferenceKeyFrame><lrt:ReferenceBoxX0>.2</lrt:ReferenceBoxX0><lrt:ReferenceBoxY0>.3</lrt:ReferenceBoxY0><lrt:ReferenceBoxX1>.8</lrt:ReferenceBoxX1><lrt:ReferenceBoxY1>.9</lrt:ReferenceBoxY1><lrt:ReferenceBoxX2>1</lrt:ReferenceBoxX2><lrt:ReferenceBoxY2>1</lrt:ReferenceBoxY2>"),null,null);
        for(boolean modern:new boolean[]{false,true})for(String value:new String[]{"0","1.23456789123","0.0000015","-0.0","NaN","Infinity","0.000000005","0.000000015","1.234567885","1.234567895","2.675000005","-1.234567885","-Infinity","-0.000000005","0.000000005000000000000001","0.000000004999999999999999","1e18","1.2345678901234568e18","1e23","-1e100","1.7976931348623157e308"}) {
            String collection=modern?"MaskGroupBasedCorrections":"GradientBasedCorrections";
            StringBuilder body=new StringBuilder("<crs:"+collection+"><rdf:Seq>");
            for(int i=1;i<=9;i++)body.append("<rdf:li rdf:parseType=\"Resource\">").append(i>=5&&i<=7?"<crs:LocalExposure2012>"+value+"</crs:LocalExposure2012>":"").append("</rdf:li>");
            body.append("</rdf:Seq></crs:"+collection+">");
            observe("internal-"+modern+"-"+value,packet(base,body.toString()),null,null);
        }
        for(String p:new String[]{"crs:Exposure2012","crs:WhiteBalance","crs:CameraProfile"}) {
            observe("uri-"+p,packet("lrt:Quality=\"RAW\"","<"+p+" rdf:resource=\"1.5\"/>"),null,null);
        }
        String ps="http://ns.adobe.com/photoshop/1.0/";
        observe("photoshop-tags",packet(base,"<ps:SidecarForExtension xmlns:ps=\""+ps+"\">jpg</ps:SidecarForExtension><ps:EmbeddedXMPDigest xmlns:ps=\""+ps+"\">synthetic</ps:EmbeddedXMPDigest><ps:DateCreated xmlns:ps=\""+ps+"\">2026-10-23</ps:DateCreated>"),null,null);
        for(String label:new String[]{"#LRT internal use (HG)","#LRT internal use (Deflicker)","#LRT internal use (Global)"}) {
            observe("mapped-"+label,packet(base,"<crs:MaskGroupBasedCorrections><rdf:Seq><rdf:li rdf:parseType=\"Resource\"><crs:CorrectionNameExtra>"+label+"</crs:CorrectionNameExtra><crs:LocalExposure2012>1.23456789123</crs:LocalExposure2012></rdf:li></rdf:Seq></crs:MaskGroupBasedCorrections>"),null,null);
        }
        for(int slot:new int[]{1,8}) {
            String p="MaskGroupBasedCorrections["+slot+"]/crs:LocalExposure2012";
            observe("create-"+slot,packet(base,""),packet(base,SelectedColumnOracle.pathBody(p,"1.23456789123")),p);
        }
        String name="MaskGroupBasedCorrections[1]/crs:CorrectionName",exposure="MaskGroupBasedCorrections[1]/crs:LocalExposure2012";
        observe("create-mapped",packet(base,""),packet(base,"<crs:MaskGroupBasedCorrections><rdf:Seq><rdf:li rdf:parseType=\"Resource\"><crs:CorrectionName>#LRT internal use (HG)</crs:CorrectionName><crs:LocalExposure2012>1.23456789123</crs:LocalExposure2012></rdf:li></rdf:Seq></crs:MaskGroupBasedCorrections>"),name+","+exposure);
        observe("qualified-numeric",packet(base,"<crs:Exposure2012 xml:lang=\"en-US\">1.23456789123</crs:Exposure2012>"),null,null);
        observe("qualified-wb",packet("lrt:Quality=\"RAW\"","<crs:WhiteBalance xml:lang=\"en-US\">Custom</crs:WhiteBalance>"),null,null);
        Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
    }
}
