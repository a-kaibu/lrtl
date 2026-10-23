package lrtlprobe;

import java.io.File;
import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;

import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of the completed CSV metadata parser and WB default helper.
// The controller shell has no constructor/UI state; neither method uses it.
// No main instance, licensing state or application bytecode is replaced.
public final class FrameStateOracle {
    static final String CRS="http://ns.adobe.com/camera-raw-settings/1.0/";
    static final String[] WB={"WhiteBalance","Temperature","Tint","IncrementalTemperature","IncrementalTint"};
    public static void main(String[] args)throws Exception {
        Locale.setDefault(Locale.JAPAN);
        Class<?> frameType=Class.forName("tQa"),registryType=Class.forName("Ooa"),controllerType=Class.forName("lrtimelapse.cOM7"),xml=Class.forName("Wd");
        Object registry=method(registryType,"A",registryType).invoke(null);
        int count=(int)method(registryType,"A",int.class).invoke(registry);
        Method index=method(frameType,"A",int.class,String.class),cell=method(frameType,"a",Object.class,int.class);
        Method parse=method(Class.forName("ABA"),"A",xml,String.class),load=method(frameType,"A",void.class,String.class,String.class,xml,int.class,boolean.class);
        Class<?> unsafeType=Class.forName("sun.misc.Unsafe");Field singleton=unsafeType.getDeclaredField("theUnsafe");singleton.setAccessible(true);
        Object shell=unsafeType.getMethod("allocateInstance",Class.class).invoke(singleton.get(null),controllerType);
        Method metadata=method(controllerType,"A",void.class,String.class,frameType),quality=method(frameType,"A",String.class),defaults=method(frameType,"h",void.class);
        if(args.length==3&&args[1].equals("--neutral")) {
            Method flatDNG=method(controllerType,"A",boolean.class,frameType);
            StringBuilder observed=new StringBuilder("[\n");String separator="";
            try(var files=Files.list(Path.of(args[2]))) {
                for(Path path:files.sorted().toList()) {
                    if(!path.getFileName().toString().toLowerCase(Locale.ROOT).endsWith(".dng"))continue;
                    Object frame=frameType.getConstructor(Object[].class).newInstance((Object)new Object[count]);method(frameType,"a",void.class,File.class).invoke(frame,path.toFile());
                    boolean flat=(boolean)flatDNG.invoke(shell,frame);
                    observed.append(separator).append("{\"filename\":").append(quote(path.getFileName().toString())).append(",\"flat\":").append(flat).append('}');separator=",\n";
                }
            }
            Files.writeString(Path.of(args[0]),observed+"\n]\n");return;
        }
        String[] names={"frame.jpg","frame.jpeg","frame.png","frame.tif","frame.tiff","frame.dng","frame.nef","frame.nrw","frame.cr2","frame.cr3","frame.arw","frame.sr2","frame.orf","frame.pef","frame.raf","frame.rw2","frame.dcr","frame.erf","frame.bmp","frame.JPG","frame.JPEG","frame.PNG","frame.TIF","frame.TIFF","frame.DNG","frame.jpg.backup","noextension",".jpg"};
        StringBuilder out=new StringBuilder("{\"metadata_completed\":true,\"defaults_completed\":true,\"controller_constructor_skipped\":true,\"neutral_tag\":50728,\"neutral_exiftool_argument\":\"-AsShotNeutral\",\"classifications\":[\n");String comma="";
        for(String filename:names)for(String neutral:new String[]{"-","","1 1 1","0.5 1 0.25","0","null"," - "}) {
            Object frame=frameType.getConstructor(Object[].class).newInstance((Object)new Object[count]);method(frameType,"A",void.class,Object[].class).invoke(frame,(Object)new Object[count]);
            method(frameType,"K",void.class,String.class).invoke(frame,"previous");
            String[] fields=new String[16];Arrays.fill(fields,"-");fields[0]=filename;fields[1]="64x32";fields[14]=neutral;
            metadata.invoke(shell,String.join(",",fields),frame);
            Object q=quality.invoke(frame);
            Class<?> files=Class.forName("lrtimelapse.tools.cOm7");
            out.append(comma).append("{\"filename\":").append(quote(filename)).append(",\"neutral\":").append(quote(neutral)).append(",\"metadata_quality\":").append(quote((String)q)).append(",\"flat_file\":");
            boolean flat=false;for(String name:new String[]{"E","B","a"})flat|=(boolean)method(files,name,boolean.class,File.class).invoke(null,new File(filename));
            out.append(flat).append('}');comma=",\n";
        }
        out.append("\n],\"references\":[\n");comma="";
        Class<?> sequenceType=Class.forName("Rsa");
        for(int[] ratings:new int[][]{{0,0},{5,4,0},{4,0,4},{5,5},{0,4,4},{2,3,0}}) {
            @SuppressWarnings("unchecked") List<Object> sequence=(List<Object>)sequenceType.getConstructor(int.class).newInstance(-1);
            for(int rating:ratings) {
                Object frame=frameType.getConstructor(Object[].class).newInstance((Object)new Object[count]);method(frameType,"E",void.class,int.class).invoke(frame,rating);sequence.add(frame);
            }
            Object selected=method(sequenceType,"A",frameType).invoke(sequence);
            int selectedIndex=-1;for(int i=0;i<sequence.size();i++)if(sequence.get(i)==selected){selectedIndex=i;break;}
            out.append(comma).append("{\"ratings\":").append(Arrays.toString(ratings)).append(",\"expected_index\":").append(selectedIndex).append('}');comma=",\n";
        }
        out.append("\n],\"defaults\":[\n");comma="";
        String[][] numbers={{null,null,null,null},{"4500.5",null,null,"12.5"},{null,"-20.5","10.5",null},{"bad","NaN","Infinity","4294967297"}};
        for(String q:new String[]{null,"","RAW","FLAT","raw","flat","JPEG","DNG"})for(String wb:new String[]{null,"As Shot","Custom","custom","CUSTOM","","Daylight"})for(String[] values:numbers) {
            StringBuilder attrs=new StringBuilder();if(wb!=null)attrs.append(" crs:WhiteBalance=\"").append(wb).append('"');
            for(int i=0;i<4;i++)if(values[i]!=null)attrs.append(" crs:").append(WB[i+1]).append("=\"").append(values[i]).append('"');
            String source="<rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description xmlns:crs=\""+CRS+"\""+attrs+"/></rdf:RDF>";
            Object frame=frameType.getConstructor(Object[].class).newInstance((Object)new Object[count]);method(frameType,"A",void.class,Object[].class).invoke(frame,(Object)new Object[count]);method(frameType,"K",void.class,String.class).invoke(frame,q);
            Object packet=parse.invoke(null,source);
            for(String name:WB)load.invoke(frame,name,CRS,packet,index.invoke(null,name),false);
            defaults.invoke(frame);
            out.append(comma).append("{\"quality\":").append(q==null?"null":quote(q)).append(",\"source\":").append(quote(source)).append(",\"expected\":{");String sep="";
            for(String name:WB){Object value=cell.invoke(frame,index.invoke(null,name));out.append(sep).append(quote(name)).append(':').append(value==null?"null":quote(value.toString()));sep=",";}
            out.append("}}");comma=",\n";
        }
        out.append("\n]}\n");Files.writeString(Path.of(args[0]),out);
    }
}
