package lrtlprobe;

import java.lang.reflect.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;
import static lrtlprobe.MetadataPasteOracle.method;
import static lrtlprobe.MetadataPasteOracle.quote;

// Original synthetic observer: installed ExifTool ImageSize -> genuine metadata
// CSV reader -> genuine camera-cache save prefix. No application/license stubs.
public final class RawDimensionsOracle {
    public static void main(String[] args)throws Exception {
        RegisteredSaveOracle.initialize();
        Class<?> controller=Class.forName("lrtimelapse.cOM7"),unsafe=Class.forName("sun.misc.Unsafe");
        Field singleton=unsafe.getDeclaredField("theUnsafe");singleton.setAccessible(true);
        Object shell=unsafe.getMethod("allocateInstance",Class.class).invoke(singleton.get(null),controller);
        Method metadata=method(controller,"A",void.class,String.class,RegisteredSaveOracle.frameType);
        ArrayList<Path> paths=new ArrayList<>();
        try(var stream=Files.list(Path.of(args[2]))) {stream.filter(p->!p.toString().endsWith(".json")).sorted().forEach(paths::add);}
        var command=new ArrayList<String>(List.of(args[1],"-T","-FileName","-ImageSize"));
        for(Path path:paths)command.add(path.toString());
        Process process=new ProcessBuilder(command).redirectError(ProcessBuilder.Redirect.DISCARD).start();
        String observed=new String(process.getInputStream().readAllBytes(),StandardCharsets.UTF_8);
        if(process.waitFor()!=0)throw new IllegalStateException("ExifTool failed");
        var lines=observed.lines().toList();
        if(lines.size()!=paths.size())throw new IllegalStateException("Missing ExifTool observations");
        String target=RegisteredSaveOracle.packet("lrt:Quality=\"RAW\" crs:HasSettings=\"False\" crs:WhiteBalance=\"As Shot\" f:Keep=\"untouched\"","");
        StringBuilder out=new StringBuilder("{\"store_completed\":false,\"stop\":\"history: null LRTimelapseMain.m()\",\"cases\":[\n");String comma="";
        for(int i=0;i<paths.size();i++) {
            Path path=paths.get(i);String[] columns=lines.get(i).split("\t",-1);
            String name=path.getFileName().toString();
            if(columns.length!=2||!columns[0].equals(name))throw new IllegalStateException("Unexpected ExifTool output");
            String size=columns[1];Object packet=RegisteredSaveOracle.parse.invoke(null,target),frame=RegisteredSaveOracle.frame(packet);
            String[] fields=new String[16];Arrays.fill(fields,"-");fields[0]=name;fields[1]=size;
            boolean invalid=false;
            try{metadata.invoke(shell,String.join(",",fields),frame);}
            catch(InvocationTargetException failure){if(!(failure.getCause() instanceof NumberFormatException))throw failure;invalid=true;}
            String saved=target;
            if(!invalid){Object sequence=RegisteredSaveOracle.sequenceType.getConstructor(int.class).newInstance(-1);method(RegisteredSaveOracle.sequenceType,"d",void.class,int.class).invoke(sequence,2);saved=RegisteredSaveOracle.save(frame,packet,sequence);}
            out.append(comma).append("{\"name\":").append(quote(name)).append(",\"data\":").append(quote(Base64.getEncoder().encodeToString(Files.readAllBytes(path)))).append(",\"size\":").append(quote(size)).append(",\"invalid\":").append(invalid).append(",\"target\":").append(quote(target)).append(",\"saved\":").append(quote(saved)).append('}');comma=",\n";
        }
        Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
    }
}
