package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;
import sun.misc.Unsafe;

/** Independent measurement of the installed centering/clipping helper. */
public class HolyGrailRangeOracle {
 public static void main(String[] args)throws Exception {
  Field singleton=Unsafe.class.getDeclaredField("theUnsafe");singleton.setAccessible(true);
  Unsafe allocator=(Unsafe)singleton.get(null);
  Class<?> c=Class.forName("lrtimelapse.cOM7");Object controller=allocator.allocateInstance(c);
  Method range=c.getMethod("A",double[].class,double.class,double.class,boolean.class);
  double[][] inputs={{},{0},{-0.0,0.0},{1,2,3},{100,100},{-100,-100},{40,41,42},{-42,-41,-40},{-100,0,100},{.12345678912345678,.1234567891234568},{1e16,1e16+2},{1e300,1.0000000000000002e300},{-1e300,-1.0000000000000002e300},{-2,-1,0,1,2}};
  System.out.print("{\"cases\":[");String sep="";
  for(double[] input:inputs)for(double[] bounds:new double[][]{{-1,1},{-10,10},{-2,7}})for(boolean clip:new boolean[]{false,true}) {
   double[] expected=(double[])range.invoke(controller,input.clone(),bounds[0],bounds[1],clip);
   for(double value:expected)if(!Double.isFinite(value))throw new IllegalStateException("non-finite result");
   System.out.print(sep+"{\"input\":"+Arrays.toString(input)+",\"lower\":"+bounds[0]+",\"upper\":"+bounds[1]+",\"clip\":"+clip+",\"expected\":"+Arrays.toString(expected)+"}");sep=",";
  }
  System.out.println("]}");
 }
}
