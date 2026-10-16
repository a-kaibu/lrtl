package lrtlprobe;
import java.lang.reflect.*;
import sun.misc.Unsafe;

/** Independent invocation of the installed adjacent-camera-settings helper. */
public class HolyGrailPairOracle {
 public static void main(String[] args)throws Exception {
  Field singleton=Unsafe.class.getDeclaredField("theUnsafe");singleton.setAccessible(true);
  Unsafe allocator=(Unsafe)singleton.get(null);
  Class<?> c=Class.forName("lrtimelapse.cOM7");Object controller=allocator.allocateInstance(c);
  Method pair=c.getDeclaredMethod("A",int.class,Double.class,double.class,double.class,int.class,int.class,double.class,double.class);pair.setAccessible(true);
  System.out.print("{\"cases\":[");String sep="";
  for(double[] shutter:new double[][]{{1.0/125,1.0/60},{1,1},{2,1}})for(int[] iso:new int[][]{{100,200},{0,200},{200,100}})for(double[] aperture:new double[][]{{8,5.6},{8,8},{0,4}}) {
   double result=(double)pair.invoke(controller,0,Double.valueOf(0),shutter[0],shutter[1],iso[0],iso[1],aperture[0],aperture[1]);
   System.out.print(sep+"{\"before\":["+shutter[0]+","+iso[0]+","+aperture[0]+"],\"after\":["+shutter[1]+","+iso[1]+","+aperture[1]+"],\"expected\":"+result+"}");sep=",";
  }
  System.out.println("]}");
 }
}
