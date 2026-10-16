package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;
import sun.misc.Unsafe;

/** Isolated synthetic call to the installed numerical curve-shaping helper.
 * Controller/table objects are allocated without starting the application UI;
 * only their synthetic sequence field is supplied. No image/XMP files are edited. */
public class HolyGrailShapeOracle {
 public static void main(String[] args)throws Exception {
  Field singleton=Unsafe.class.getDeclaredField("theUnsafe");singleton.setAccessible(true);
  Unsafe allocator=(Unsafe)singleton.get(null);
  Class<?> f=Class.forName("tQa"),s=Class.forName("Rsa"),c=Class.forName("lrtimelapse.cOM7"),t=Class.forName("lrtimelapse.com8");
  Object controller=allocator.allocateInstance(c),table=allocator.allocateInstance(t);
  Field tableSequence=t.getDeclaredField("M");tableSequence.setAccessible(true);
  Field controllerTable=c.getDeclaredField("wc");controllerTable.setAccessible(true);controllerTable.set(controller,table);
  Field preview=f.getDeclaredField("O");preview.setAccessible(true);
  Method shape=Arrays.stream(c.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==double[].class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{double.class,double.class,double.class})).findFirst().orElseThrow();
  double[][] inputs={{0,.1,.6,.7,.8},{0,.1,.6,.7,.8},{0,.1,.6,.7,.8},{2,2.5,4.5,5,5.5,6},{3,2.8,2.6,1.4,1.2,1},{-4,-3,-2,0,1,3},{10,10.25,13,13.25,13.5}};
  int[][] ratings={{0,2,3,0,0},{0,4,4,0,0},{0,2,0,3,0},{2,3,0,0,2,3},{0,2,3,2,3,0},{0,2,3,4,2,3},{2,3,2,3,0}};
  System.out.print("{\"cases\":[");String sep="";
  for(int k=0;k<inputs.length;k++)for(double stretch:new double[]{-1.5,0,.82,1.5})for(double rotate:new double[]{-4,0,2.36,4}) {
   @SuppressWarnings("unchecked") List<Object> sequence=(List<Object>)s.getConstructor(int.class).newInstance(-1);
   for(int i=0;i<inputs[k].length;i++){Object frame=f.getConstructor(Object[].class).newInstance((Object)new Object[4096]);preview.set(frame,Double.valueOf(inputs[k][i]));f.getMethod("E",int.class).invoke(frame,ratings[k][i]);sequence.add(frame);}
   tableSequence.set(table,sequence);
   double[] expected=(double[])shape.invoke(controller,stretch,0.0,rotate);
   for(double value:expected)if(!Double.isFinite(value))throw new IllegalStateException("non-finite case "+k);
   System.out.print(sep+"{\"input\":"+Arrays.toString(inputs[k])+",\"ratings\":"+Arrays.toString(ratings[k])+",\"stretch\":"+stretch+",\"rotate\":"+rotate+",\"expected\":"+Arrays.toString(expected)+"}");sep=",";
  }
  System.out.println("]}");
 }
}
