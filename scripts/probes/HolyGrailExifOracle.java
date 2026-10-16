package lrtlprobe;

import java.lang.reflect.*;
import java.nio.*;
import java.nio.file.*;
import java.util.*;

/** Measures installed ExifTool display values followed by native HG parsers.
 * Pass the installed exiftool executable as the first argument. Temporary
 * synthetic metadata-only JPEGs are removed after the measurement.
 */
public class HolyGrailExifOracle {
 public static void main(String[] args) throws Exception {
  Class.forName("tQa");
  Class<?> parser=Class.forName("lrtimelapse.tools.c");
  Method number=parser.getDeclaredMethod("a",String.class), aperture=parser.getDeclaredMethod("A",String.class);
  int[][] times={{1,8000},{1,4000},{1,2000},{1,1000},{1,500},{1,250},{1,200},{1,160},{1,125},{1,100},{1,80},{1,60},{1,50},{1,40},{1,30},{1,25},{1,20},{1,15},{1,13},{1,10},{1,8},{1,6},{1,5},{1,4},{250005,1000000},{25001,100000},{3,10},{4,10},{5,10},{6,10},{8,10},{1,1},{13,10},{16,10},{2,1},{25,10},{3,1},{4,1},{5,1},{6,1},{8,1},{10,1},{15,1},{30,1},{12345,1000},{225,100},{235,100},{123,100000},{987,100000}};
  int[][] fNumbers={{1,1},{12,10},{14,10},{18,10},{28,10},{4,1},{45,10},{5,1},{56,10},{63,10},{71,10},{8,1},{9,1},{11,1},{13,1},{16,1},{22,1},{32,1},{95,100},{85,100},{225,100},{235,100},{5656854,1000000},{16,10},{2,1},{22,10},{25,10},{32,10},{35,10},{10,1},{14,1},{18,1},{20,1},{25,1},{29,1},{36,1},{40,1},{45,1},{51,1},{57,1},{64,1},{72,1},{80,1},{90,1},{55,10}};
  Path dir=Files.createTempDirectory("lrtl-hg-exif-");
  List<int[]> inputs=new ArrayList<>();
  for(int[] time:times) inputs.add(new int[]{time[0],time[1],56,10,100});
  for(int[] f:fNumbers) inputs.add(new int[]{1,125,f[0],f[1],125});
  List<String> command=new ArrayList<>(List.of(args[0],"-T","-ShutterSpeed","-Aperture","-ISO"));
  try {
   for(int i=0;i<inputs.size();i++) {
    Path file=dir.resolve(String.format(Locale.ROOT,"frame%04d.jpg",i));Files.write(file,jpeg(inputs.get(i)));command.add(file.toString());
   }
   ProcessBuilder builder=new ProcessBuilder(command);builder.redirectError(ProcessBuilder.Redirect.INHERIT);Process process=builder.start();
   String output=new String(process.getInputStream().readAllBytes(),java.nio.charset.StandardCharsets.UTF_8);
   if(process.waitFor()!=0)throw new IllegalStateException("ExifTool failed");
   String[] lines=output.strip().split("\\R");if(lines.length!=inputs.size())throw new IllegalStateException("unexpected row count: "+lines.length);
   StringBuilder json=new StringBuilder("{\"cases\":[");String sep="";
   for(int i=0;i<inputs.size();i++) {
    String[] labels=lines[i].split("\\t");int[] in=inputs.get(i);
    double time=(double)number.invoke(null,labels[0]),f=(double)aperture.invoke(null,labels[1]);int iso=(int)(double)number.invoke(null,labels[2]);
    json.append(sep).append("{\"timeNumerator\":").append(in[0]).append(",\"timeDenominator\":").append(in[1]).append(",\"apertureNumerator\":").append(in[2]).append(",\"apertureDenominator\":").append(in[3]).append(",\"iso\":").append(in[4]);
    json.append(",\"shutterLabel\":\"").append(labels[0]).append("\",\"apertureLabel\":\"").append(labels[1]).append("\",\"expectedTime\":").append(time).append(",\"expectedAperture\":").append(f).append(",\"expectedISO\":").append(iso).append("}");sep=",";
   }
   System.out.println(json.append("]}"));
  } finally {try(var files=Files.list(dir)){for(Path file:files.toList())Files.delete(file);}Files.delete(dir);}
 }
 static byte[] jpeg(int[] values) {
  ByteBuffer t=ByteBuffer.allocate(84).order(ByteOrder.LITTLE_ENDIAN);t.put(new byte[]{'I','I',42,0}).putInt(8).putShort((short)1);entry(t,0x8769,4,26);t.position(26);t.putShort((short)3);entry(t,0x829a,5,68);entry(t,0x829d,5,76);entry(t,0x8827,3,values[4]);t.position(68);for(int i=0;i<4;i++)t.putInt(values[i]);
  ByteBuffer j=ByteBuffer.allocate(98);j.putShort((short)0xffd8).putShort((short)0xffe1).putShort((short)92).put(new byte[]{'E','x','i','f',0,0}).put(t.array()).putShort((short)0xffd9);return j.array();
 }
 static void entry(ByteBuffer b,int tag,int kind,int value){b.putShort((short)tag).putShort((short)kind).putInt(1).putInt(value);}
}
