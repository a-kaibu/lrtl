package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;
/** Independent caller of installed LRT 7.5.5 spline/rounding components. */
public class TransitionOracle {
 public static void main(String[] args)throws Exception {
  Class<?> pointType=Class.forName("OC"), splineType=Class.forName("de");
  Constructor<?> point=pointType.getConstructor(float.class,float.class);
  Constructor<?> spline=splineType.getConstructor(List.class,Class.forName("pB"),float.class);
  Method sample=splineType.getMethod("A",int.class);
  Field x=pointType.getField("j"),y=pointType.getField("l");
  Method round=Class.forName("lrtimelapse.tools.com3").getMethod("A",double.class,int.class);
  System.out.print("{\"rounding\":[");String separator="";
  for(double v:new double[]{-100.5,-2.5,-1.5,-.5,-.499999,0,.499999,.5,1.5,2.5,100.5}) {
   System.out.print(separator+"{\"input\":"+v+",\"expected\":"+round.invoke(null,v,0)+"}");separator=",";
  }
  System.out.print("],\"decimalRounding\":[");separator="";
  for(double v:new double[]{.00005,.00015,.00025,.00035,.12345,1.23445,1.23455,-.00005,-.00015,-1.23445,-1.23455,1.00004999,1.00005001}) {
   System.out.print(separator+"{\"input\":"+v+",\"expected\":"+round.invoke(null,v,4)+"}");separator=",";
  }
  System.out.print("],\"cases\":[");separator="";
  for(int length:new int[]{5,17,101})for(int kind=0;kind<3;kind++)for(int smoothness:new int[]{0,50,100}) {
   int[] positions=kind==0?new int[]{0,length/2,length-1}:kind==1?new int[]{0,length/3,length-1}:new int[]{0,length/4,length*3/4,length-1};
   float[] values=kind==0?new float[]{0,1.75f,3.5f}:kind==1?new float[]{-1,2,-2}:new float[]{-100,100,-50,75};
   List<Object> points=new ArrayList<>();boolean[] occupied=new boolean[length];
   for(int i=0;i<positions.length;i++){points.add(point.newInstance((float)positions[i],values[i]));occupied[positions[i]]=true;}
   Object curve=spline.newInstance(points,null,(100-smoothness)/500f);
   List<?> output=(List<?>)sample.invoke(curve,length*2);
   Double[] bins=new Double[length];
   for(Object p:output){int frame=Math.max(0,Math.min(length-1,(int)x.getFloat(p)));if(!occupied[frame]){bins[frame]=(double)y.getFloat(p);occupied[frame]=true;}}
   System.out.print(separator+"\n{\"length\":"+length+",\"smoothness\":"+smoothness+",\"positions\":"+Arrays.toString(positions)+",\"values\":"+Arrays.toString(values)+",\"expected\":"+Arrays.toString(bins)+"}");separator=",";
  }
  System.out.println("\n]}");
 }
}

