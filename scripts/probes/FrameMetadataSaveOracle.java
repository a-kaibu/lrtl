package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original observer of CSV metadata-cache parsing followed by the genuine save
// prefix. HasSettings=False isolates camera metadata from wall-clock date writes.
// No main, license or proprietary method is replaced.
public final class FrameMetadataSaveOracle {
    public static void main(String[] args)throws Exception {
        RegisteredSaveOracle.initialize();
        Class<?> controller=Class.forName("lrtimelapse.cOM7"),unsafe=Class.forName("sun.misc.Unsafe");
        Field one=unsafe.getDeclaredField("theUnsafe");one.setAccessible(true);
        Object shell=unsafe.getMethod("allocateInstance",Class.class).invoke(one.get(null),controller);
        Method metadata=method(controller,"A",void.class,String.class,RegisteredSaveOracle.frameType);
        String target=RegisteredSaveOracle.packet("lrt:Quality=\"RAW\" crs:HasSettings=\"False\" crs:WhiteBalance=\"As Shot\" f:Keep=\"unchanged\"","");
        int[] positions={1,3,4,5,7,10,13};
        String[][] values={
            {"-","64x32","0x0","-1x2","2147483647x1","2147483648x1","64","64X32","badx2"," 2x3","2x3x4","2x","x3","2xx3","2x3x","x"},
            {"-","","5.6","0","-1","NaN","Infinity","1e50","n/a","N/A","bad"," 5.6 ","5.6f","0x1.8p1"},
            {"-","","1/125","1.5","Bulb","bad"," 1/2 "},
            {"-","","200","0","bad"," 100 "},
            {"-","","Manual","Auto","manual","  Manual  ","n/a"},
            {"-","","1","0","8","9","-1","2147483647","2147483648"," 6 ","bad","+6","６","+ 6","++6","+-6"},
            {"-","","true","TRUE","True","false","1"," true ","yes"}
        };
        StringBuilder out=new StringBuilder("{\"metadata_completed\":true,\"store_completed\":false,\"stop\":\"history: null LRTimelapseMain.m()\",\"cases\":[\n");
        String comma="";
        for(int group=0;group<positions.length;group++)for(String value:values[group]) {
            String[] fields=new String[16];Arrays.fill(fields,"-");fields[0]="frame.nef";fields[1]="64x32";fields[positions[group]]=value;
            Object packet=RegisteredSaveOracle.parse.invoke(null,target),frame=RegisteredSaveOracle.frame(packet);
            boolean invalid=false;
            try {metadata.invoke(shell,csv(fields),frame);}
            catch(InvocationTargetException failure) {
                if(!(failure.getCause() instanceof NumberFormatException))throw failure;
                invalid=true;
            }
            String saved=target;
            if(!invalid) {
                Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);
                method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);
                saved=RegisteredSaveOracle.save(frame,packet,sequence);
            }
            out.append(comma).append("{\"name\":").append(quote(positions[group]+":"+value)).append(",\"fields\":[");
            for(int i=0;i<fields.length;i++){if(i>0)out.append(',');out.append(quote(fields[i]));}
            out.append("],\"invalid\":").append(invalid).append(",\"target\":").append(quote(target)).append(",\"saved\":").append(quote(saved)).append('}');comma=",\n";
        }
        Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
    }
    static String csv(String[] fields) {
        StringJoiner joined=new StringJoiner(",");
        for(String field:fields)joined.add("\""+field.replace("\"","\"\"")+"\"");
        return joined.toString();
    }
}
