package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;

/** Independent call to the installed sparse numeric transition component. */
public class SparseTransitionOracle {
 public static void main(String[] args)throws Exception {
  Object linear=Class.forName("bqa").getConstructor().newInstance();
  Method interpolate=linear.getClass().getMethod("A",ArrayList.class,Class.class);
  System.out.print("{\"cases\":[");String separator="";
  for(int count:new int[]{3,5,9,17,101})for(int kind=0;kind<4;kind++) {
   boolean integer=kind>=2;
   double start=kind==0?-1.25:kind==1?-.700000000123:kind==2?-7:-2;
   double end=kind==0?3.5:kind==1?.900000000987:kind==2?-2:-7;
   ArrayList<Object> input=new ArrayList<>();
   for(int i=0;i<count;i++) {
    double v=i==0?start:i==count-1?end:99;
    if(integer)input.add(Integer.valueOf((int)v));else input.add(Double.valueOf(v));
   }
   List<?> output=(List<?>)interpolate.invoke(linear,input,integer?Integer.class:Double.class);
   System.out.print(separator+"{\"count\":"+count+",\"integer\":"+integer+",\"start\":"+start+",\"end\":"+end+",\"expected\":"+output+"}");separator=",";
  }
  System.out.println("]}");
 }
}
