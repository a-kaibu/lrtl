package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;
import javax.swing.*;

import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original synthetic observer of the numeric-edit callback prefix. The editor
// shell is allocated without its GUI constructor, and receives genuine native
// slider controls and a synthetic frame. Its controller stays null: every call
// must stop at that exact dependency after the observed cache assignments.
// This does not run the full GUI, initialize EXIF defaults, or alter licensing.
public final class WBEditOracle {
    static final String CRS="http://ns.adobe.com/camera-raw-settings/1.0/";
    static final String[] NUMBERS={"Temperature","Tint","IncrementalTemperature","IncrementalTint"};
    static final String[] FIELDS={"y","M","O","a"};
    static final int[] DEFAULTS={5850,8,0,0};
    static void put(Object owner,String name,Object value)throws Exception {
        Field field=owner.getClass().getDeclaredField(name);field.setAccessible(true);field.set(owner,value);
    }
    public static void main(String[] args)throws Exception {
        Class<?> frameType=Class.forName("tQa"),regType=Class.forName("Ooa"),editorType=Class.forName("qqa"),epa=Class.forName("Epa"),base=Class.forName("dRa"),sequence=Class.forName("Rsa"),xml=Class.forName("Wd");
        Object registry=method(regType,"A",regType).invoke(null);
        Method lookup=method(regType,"A",epa,String.class),index=method(frameType,"A",int.class,String.class),cell=method(frameType,"a",Object.class,int.class);
        Method load=method(frameType,"A",void.class,String.class,String.class,xml,int.class,boolean.class);
        Method parse=method(Class.forName("ABA"),"A",xml,String.class);
        Method callback=method(editorType,"A",void.class,epa,double.class);
        Class<?> unsafeType=Class.forName("sun.misc.Unsafe");Field singleton=unsafeType.getDeclaredField("theUnsafe");singleton.setAccessible(true);Object unsafe=singleton.get(null);
        int count=(int)method(regType,"A",int.class).invoke(registry);
        StringBuilder out=new StringBuilder("{\"callback_completed\":false,\"editor_constructor_skipped\":true,\"stop\":\"null editor controller g\",\"bounds\":[");
        for(int i=0;i<4;i++) {
            Object descriptor=lookup.invoke(registry,NUMBERS[i]);
            if(i>0)out.append(',');
            out.append("{\"property\":").append(quote(NUMBERS[i])).append(",\"min\":").append(method(epa,"a",double.class,sequence).invoke(descriptor,new Object[]{null})).append(",\"max\":").append(method(epa,"A",double.class,sequence).invoke(descriptor,new Object[]{null})).append(",\"fallback\":").append(DEFAULTS[i]).append('}');
        }
        out.append("],\"cases\":[\n");String separator="";
        String[][] baselines={{null,null,null,null},{"4500.5","20.5","-15.5","9.5"},{"90000","-999","NaN","4294967297"}};
        double[] requests={-100000,0.9,21.9,6500.9,100000};
        for(String quality:new String[]{null,"RAW","FLAT","flat"})for(String wb:new String[]{null,"As Shot","Custom","Daylight"})for(String[] values:baselines)for(int edited=0;edited<4;edited++)for(double requested:requests) {
            StringBuilder attributes=new StringBuilder();
            if(quality!=null)attributes.append(" lrt:Quality=\"").append(quality).append('"');
            if(wb!=null)attributes.append(" crs:WhiteBalance=\"").append(wb).append('"');
            for(int i=0;i<4;i++)if(values[i]!=null)attributes.append(" crs:").append(NUMBERS[i]).append("=\"").append(values[i]).append('"');
            String source="<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description xmlns:crs=\""+CRS+"\" xmlns:lrt=\"http://lrtimelapse.com/\" xmlns:f=\"urn:lrtl:wb-edit/\""+attributes+"><f:Nested><rdf:Description crs:Temperature=\"777\" crs:Tint=\"99\"/></f:Nested></rdf:Description></rdf:RDF></x:xmpmeta>";
            Object frame=frameType.getConstructor(Object[].class).newInstance((Object)new Object[count]);method(frameType,"A",void.class,Object[].class).invoke(frame,(Object)new Object[count]);method(frameType,"K",void.class,String.class).invoke(frame,quality);
            Object packet=parse.invoke(null,source);
            for(String name:new String[]{"WhiteBalance","Temperature","Tint","IncrementalTemperature","IncrementalTint"})load.invoke(frame,name,CRS,packet,index.invoke(null,name),false);
            Object editor=unsafeType.getMethod("allocateInstance",Class.class).invoke(unsafe,editorType);put(editor,"x",frame);
            JSlider[] sliders=new JSlider[4];
            for(int i=0;i<4;i++) {
                Object descriptor=lookup.invoke(registry,NUMBERS[i]);
                Object control=Class.forName(i==0?"Cqa":"GSa").getConstructor().newInstance();
                JSlider slider=(JSlider)method(base,"A",JSlider.class).invoke(control);
                slider.setMinimum((int)(double)method(epa,"a",double.class,sequence).invoke(descriptor,new Object[]{null}));
                slider.setMaximum((int)(double)method(epa,"A",double.class,sequence).invoke(descriptor,new Object[]{null}));
                Object current=cell.invoke(frame,index.invoke(null,NUMBERS[i]));slider.setValue(current==null?DEFAULTS[i]:(Integer)current);
                sliders[i]=slider;put(editor,FIELDS[i],control);
            }
            sliders[edited].setValue((int)requested);
            try {callback.invoke(editor,lookup.invoke(registry,NUMBERS[edited]),(double)sliders[edited].getValue());throw new IllegalStateException("unexpected callback completion");}
            catch(InvocationTargetException failure) {
                Throwable cause=failure.getCause();
                if(!(cause instanceof NullPointerException)||!cause.toString().contains(".g\" is null"))throw failure;
            }
            out.append(separator).append("{\"source\":").append(quote(source)).append(",\"property\":").append(quote(NUMBERS[edited])).append(",\"requested\":").append(requested).append(",\"expected\":{");
            String comma="";
            for(String name:new String[]{"WhiteBalance","Temperature","Tint","IncrementalTemperature","IncrementalTint"}) {
                Object value=cell.invoke(frame,index.invoke(null,name));
                out.append(comma).append(quote(name)).append(':').append(value==null?"null":quote(value.toString()));comma=",";
            }
            out.append("}}");separator=",\n";
        }
        out.append("\n]}\n");Files.writeString(Path.of(args[0]),out);
    }
}
