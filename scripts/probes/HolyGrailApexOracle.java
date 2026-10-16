package lrtlprobe;

import java.lang.reflect.*;
import java.nio.*;
import java.nio.file.*;
import java.nio.charset.StandardCharsets;
import java.util.*;

/** Installed ExifTool composite labels followed by installed HG parsers.
 * Uses temporary synthetic EXIF; no user images or application settings change.
 */
public class HolyGrailApexOracle {
 static int[] tag(int id,int type,int n,int d){return new int[]{id,type,n,d};}
 static int[] tv(int n,int d){return tag(0x9201,10,n,d);}
 static int[] av(int n,int d){return tag(0x9202,5,n,d);}
 public static void main(String[] args)throws Exception {
  Class.forName("tQa");Class<?> parser=Class.forName("lrtimelapse.tools.c");
  Method number=parser.getDeclaredMethod("a",String.class),aperture=parser.getDeclaredMethod("A",String.class);
  List<int[][]> cases=new ArrayList<>();
  for(int i=-15;i<=39;i++)cases.add(new int[][]{tv(i,3),av(6,1)});
  for(int i=-12;i<=36;i++)cases.add(new int[][]{tv(7,1),tag(0x9202,i<0?10:5,i,3)});
  for(int i:new int[]{-100,100,101})cases.add(new int[][]{tv(i,1),av(6,1)});
  cases.add(new int[][]{tag(0x9201,5,-3,1),av(6,1)}); // ExifTool's signed-value correction.
  for(boolean reverse:new boolean[]{false,true}) {
   int[][] entries={tag(0x829a,5,1,125),tag(0x829d,5,56,10),tv(1,1),av(2,1)};
   if(reverse)Collections.reverse(Arrays.asList(entries));cases.add(entries);
   entries=new int[][]{tag(0x829a,5,0,1),tag(0x829d,5,0,1),tv(7,1),av(5,1)};
   if(reverse)Collections.reverse(Arrays.asList(entries));cases.add(entries);
  }
  Path dir=Files.createTempDirectory("lrtl-hg-apex-");
  List<String> command=new ArrayList<>(List.of(args[0],"-T","-ShutterSpeed","-Aperture","-ISO"));
  try {
   for(int i=0;i<cases.size();i++){Path file=dir.resolve(String.format(Locale.ROOT,"frame%04d.jpg",i));Files.write(file,jpeg(cases.get(i)));command.add(file.toString());}
   ProcessBuilder builder=new ProcessBuilder(command);builder.redirectError(ProcessBuilder.Redirect.INHERIT);Process p=builder.start();String output=new String(p.getInputStream().readAllBytes(),StandardCharsets.UTF_8);if(p.waitFor()!=0)throw new IllegalStateException("ExifTool failed");
   String[] lines=output.strip().split("\\R");if(lines.length!=cases.size())throw new IllegalStateException("row count mismatch");
   StringBuilder json=new StringBuilder("{\"cases\":[");String sep="";
   for(int i=0;i<cases.size();i++) {
    String[] labels=lines[i].split("\\t");double time=(double)number.invoke(null,labels[0]),f=(double)aperture.invoke(null,labels[1]);
    if(!Double.isFinite(time)||!Double.isFinite(f))throw new IllegalStateException("non-finite measurement");
    json.append(sep).append("{\"tags\":").append(Arrays.deepToString(cases.get(i))).append(",\"shutterLabel\":\"").append(labels[0]).append("\",\"apertureLabel\":\"").append(labels[1]).append("\",\"expectedTime\":").append(time).append(",\"expectedAperture\":").append(f).append("}");sep=",";
   }
   System.out.println(json.append("]}"));
  } finally {try(var files=Files.list(dir)){for(Path file:files.toList())Files.delete(file);}Files.delete(dir);}
 }
 static byte[] jpeg(int[][] tags) {
  int rationals=26+2+(tags.length+1)*12+4;
  ByteBuffer t=ByteBuffer.allocate(rationals+tags.length*8).order(ByteOrder.LITTLE_ENDIAN);
  t.put(new byte[]{'I','I',42,0}).putInt(8).putShort((short)1).putShort((short)0x8769).putShort((short)4).putInt(1).putInt(26);t.position(26);t.putShort((short)(tags.length+1));
  for(int i=0;i<tags.length;i++){int[] v=tags[i];t.putShort((short)v[0]).putShort((short)v[1]).putInt(1).putInt(rationals+i*8);}
  t.putShort((short)0x8827).putShort((short)3).putInt(1).putInt(100);t.position(rationals);for(int[] v:tags)t.putInt(v[2]).putInt(v[3]);
  ByteBuffer j=ByteBuffer.allocate(t.capacity()+14);j.putShort((short)0xffd8).putShort((short)0xffe1).putShort((short)(t.capacity()+8)).put(new byte[]{'E','x','i','f',0,0}).put(t.array()).putShort((short)0xffd9);return j.array();
 }
}
