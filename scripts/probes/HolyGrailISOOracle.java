package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;

/** Measures the installed numeric-string parser and Java int conversion used
 * for ISO by the HG frame-pair wrapper, not an EXIF metadata decoder. */
public class HolyGrailISOOracle {
 public static void main(String[] args)throws Exception {
  Method parse=Class.forName("lrtimelapse.tools.c").getDeclaredMethod("a",String.class);
  System.out.print("{\"cases\":[");String sep="";
  Set<Double> inputs=new LinkedHashSet<>();
  for(double value:new double[]{0,-1,25,32,40,50,64,80,100,125,160,200,250,320,400,500,640,800,1000,1250,1600,2000,2500,3200,4000,5000,6400,8000,10000,12500,12800,16000,20000,25000,25600,32000,40000,50000,51200,64000,80000,100000,102400,204800,123,100.5,3e9})inputs.add(value);
  for(int value=1;value<100;value++)inputs.add((double)value);
  for(double value:inputs) {
   String text=value==Math.rint(value)?String.format(java.util.Locale.ROOT,"%.0f",value):Double.toString(value);
   double parsed=(double)parse.invoke(null,text);
   System.out.print(sep+"{\"input\":"+value+",\"parsed\":"+parsed+",\"expected\":"+(int)parsed+"}");sep=",";
  }
  System.out.println("]}");
 }
}
