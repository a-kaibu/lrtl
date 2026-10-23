package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;

import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original synthetic observer of registry types, column loading and storing.
// This invokes metadata components; it does not construct the GUI controller.
public final class SelectedColumnOracle {
    static final String CRS="http://ns.adobe.com/camera-raw-settings/1.0/";
    static String pathBody(String property,String value) {
        String[] parts=property.split("/",2);
        String step=parts[0],tag=step.contains(":")?step:"crs:"+step;
        int bracket=tag.indexOf('[');
        if(bracket>=0) {
            int index=Integer.parseInt(tag.substring(bracket+1,tag.length()-1));tag=tag.substring(0,bracket);
            StringBuilder body=new StringBuilder("<"+tag+"><rdf:Seq>");
            for(int i=1;i<=index;i++)body.append("<rdf:li rdf:parseType=\"Resource\">").append(i==index&&parts.length>1?pathBody(parts[1],value):"").append("</rdf:li>");
            return body.append("</rdf:Seq></"+tag+">").toString();
        }
        return "<"+tag+(parts.length>1?" rdf:parseType=\"Resource\"":"")+">"+(parts.length>1?pathBody(parts[1],value):value)+"</"+tag+">";
    }
    static String packet(String property,String text,boolean array) {
        String body="";
        if(text!=null) {
            body=pathBody(property,array?text:text.replace("&","&amp;").replace("<","&lt;"));
        }
        return "<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description rdf:about=\"\" xmlns:crs=\""+CRS+"\" xmlns:crd=\"http://ns.adobe.com/camera-raw-defaults/1.0/\" xmlns:tiff=\"http://ns.adobe.com/tiff/1.0/\" xmlns:exif=\"http://ns.adobe.com/exif/1.0/\" xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\" xmlns:lrt=\"http://lrtimelapse.com/\">"+body+"</rdf:Description></rdf:RDF></x:xmpmeta>";
    }
    public static void main(String[] args)throws Exception {
        Locale.setDefault(Locale.JAPAN);
        Class<?> frame=Class.forName("tQa"),reg=Class.forName("Ooa"),xml=Class.forName("Wd"),factory=Class.forName("ABA"),options=Class.forName("bCA");
        Object registry=method(reg,"A",reg).invoke(null);
        int count=(int)method(reg,"A",int.class).invoke(registry);
        Method property=method(reg,"a",String.class,int.class),type=method(reg,"A",Class.class,int.class);
        Method parse=method(factory,"A",xml,String.class),serialize=method(factory,"A",String.class,xml,options);
        Method read=method(xml,"A",String.class,String.class,String.class);
        Method load=method(frame,"A",void.class,String.class,String.class,xml,int.class,boolean.class);
        Method set=method(frame,"a",void.class,int.class,Object.class),get=method(frame,"a",Object.class,int.class);
        Method store=method(frame,"A",void.class,Class.forName("Rsa"),ArrayList.class,boolean.class);
        Field packetField=frame.getDeclaredField("Aa"),baseline=frame.getDeclaredField("n");packetField.setAccessible(true);baseline.setAccessible(true);
        Map<String,Integer> indices=new LinkedHashMap<>();
        StringBuilder out=new StringBuilder("{\"registry\":[");
        for(int i=0;i<count;i++) {
            String name=(String)property.invoke(registry,i),kind=((Class<?>)type.invoke(registry,i)).getSimpleName();indices.put(name,i);
            if(i>0)out.append(',');out.append("{\"index\":").append(i).append(",\"property\":").append(quote(name)).append(",\"kind\":").append(quote(kind)).append('}');
        }
        if(args.length==2&&args[1].equals("--registry")) {
            Files.writeString(Path.of(args[0]),out.substring("{\"registry\":".length())+"]\n");return;
        }
        Object sequence=Class.forName("Rsa").getConstructor(int.class).newInstance(-1);
        if(args.length==2&&args[1].equals("--paths")) {
            String[] names={"GradientBasedCorrections[1]/crs:LocalExposure2012","GradientBasedCorrections[4]/crs:LocalExposure2012","GradientBasedCorrections[4]/crs:CorrectionRangeMask/crs:LumMin","MaskGroupBasedCorrections[11]/crs:LocalExposure2012","MaskGroupBasedCorrections[1]/crs:CorrectionMasks[1]/crs:Left","MaskGroupBasedCorrections[8]/crs:CorrectionMasks[1]/crs:Left","MaskGroupBasedCorrections[8]/crs:CorrectionMasks[1]/crs:Left","MaskGroupBasedCorrections[8]/crs:CorrectionMasks[1]/crs:Left","MaskGroupBasedCorrections[8]/crs:CorrectionMasks[1]/crs:Left"};
            String[] parents={"","","","","","MaskGroupBasedCorrections[8]/crs:Kept","MaskGroupBasedCorrections[7]/crs:Kept","MaskGroupBasedCorrections[1]/crs:Kept","MaskGroupBasedCorrections[8]/crs:CorrectionMasks[1]/crs:Left"};
            StringBuilder pathsOut=new StringBuilder("[\n");
            for(int i=0;i<names.length;i++) {
                String targetText=parents[i].isEmpty()?packet(names[i],null,false):packet(parents[i],"recipient",false);
                Object target=parse.invoke(null,targetText),destination=frame.getConstructor(Object[].class).newInstance((Object)new Object[count]);
                baseline.set(destination,new Object[count]);packetField.set(destination,target);set.invoke(destination,5,Double.valueOf(.25));
                ArrayList<String> paths=new ArrayList<>(Collections.nCopies(5,"unused"));paths.add(names[i]);String error="";
                try {store.invoke(destination,sequence,paths,false);} catch(InvocationTargetException failure){error=failure.getCause().toString();}
                method(Class.forName("xOa"),"A",void.class,xml,String.class).invoke(null,target,"http://ns.adobe.com/xap/1.0/mm/");
                String expected=(String)serialize.invoke(null,target,options.getConstructor(int.class).newInstance(16));
                if(i>0)pathsOut.append(",\n");
                pathsOut.append("{\"property\":").append(quote(names[i])).append(",\"destination\":").append(quote(targetText)).append(",\"source\":").append(quote(packet(names[i],".25",false))).append(",\"expected\":").append(quote(expected)).append(",\"error\":").append(quote(error)).append('}');
            }
            Files.writeString(Path.of(args[0]),pathsOut.append("\n]\n"));return;
        }
        List<String[]> cases=new ArrayList<>();
        for(String value:new String[]{"0.123456789123","0.0000005","-0.0000005","1,25"," 1.2 ","0x1.8p-2f","bad","",null,"1e308","1e-300","-0.0","NaN","Infinity"}) cases.add(new String[]{"Exposure2012",value});
        for(String value:new String[]{"12.5","-12.5","2147483648","-2147483649","bad","1,5","1e3","0x1p3","1f"," 12 ","",null,"NaN","Infinity"}) cases.add(new String[]{"Tint",value});
        for(String value:new String[]{"+ 1.25","  + 1.25","0x1p0D","1D","+NaN","-NaN","inf","-Infinity","0x1p1024","0.9999995","2.4999995","1.2345675"}) cases.add(new String[]{"Exposure2012",value});
        for(String value:new String[]{"１２","١٢","+ 12","  + 12","1_000","\u00a012\u00a0","9223372036854775808","-9223372036854775809"}) cases.add(new String[]{"Tint",value});
        Random random=new Random(0x1a6bL);
        for(int i=0;i<64;i++) {
            double value=Double.longBitsToDouble(random.nextLong());
            if(Double.isFinite(value))cases.add(new String[]{"Exposure2012",Double.toString(value)});
        }
        for(String name:new String[]{"WhiteBalance","HasCrop","crs:CameraProfile","lrt:ExternalExposureDefault"}) for(String value:new String[]{""," ","As Shot","false","TRUE","日本語",null}) cases.add(new String[]{name,value});
        for(int i=5;i<count;i++) {
            String name=(String)property.invoke(registry,i),kind=((Class<?>)type.invoke(registry,i)).getSimpleName();
            cases.add(new String[]{name,kind.equals("Double")?".123456789123":kind.equals("Integer")?"-12.5":"synthetic"});
        }
        out.append("],\"cases\":[");int number=0;String commonError=null;
        for(String[] c:cases) {
            String name=c[0],sourceText=packet(name,c[1],c.length>2),destinationText=packet(name,"7",false);
            Object source=frame.getConstructor(Object[].class).newInstance((Object)new Object[count]);
            Object destination=frame.getConstructor(Object[].class).newInstance((Object)new Object[count]);
            Object saved=parse.invoke(null,destinationText);
            baseline.set(destination,new Object[count]);packetField.set(destination,saved);
            int index=indices.get(name);Object value=null;String error="",stored=null;
            try {
                load.invoke(source,name,CRS,parse.invoke(null,sourceText),index,false);
                value=get.invoke(source,index);
                // The observed false/full-paste branch copies the selected cell.
                set.invoke(destination,5,value);
                ArrayList<String> paths=new ArrayList<>(Collections.nCopies(5,"unused"));paths.add("ProbeValue");
                store.invoke(destination,sequence,paths,false);
            } catch(InvocationTargetException failure) { error=failure.getCause().toString(); }
            // The later history/UI stage can be unavailable outside the app.
            // Observe the already written test cell without claiming that the
            // entire save operation returned successfully.
            stored=(String)read.invoke(saved,CRS,"ProbeValue");
            if(commonError==null)commonError=error;
            if(!error.equals(commonError)||!error.contains("LRTimelapseMain.m()"))throw new IllegalStateException("Unexpected post-store stage: "+error);
            if(number++>0)out.append(',');
            out.append("{\"property\":").append(quote(name)).append(",\"source\":").append(quote(sourceText));
            out.append(",\"cell\":").append(value==null?"null":quote(value.toString())).append(",\"stored\":").append(stored==null?"null":quote(stored)).append('}');
        }
        Files.writeString(Path.of(args[0]),out.append("],\"save_completed\":false,\"post_store_error\":").append(quote(commonError)).append("}\n"));
        System.out.println("OBSERVED "+count+" REGISTERED COLUMNS AND "+number+" LOAD/STORE CASES");
    }
}
