package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;

/** Independently invokes the installed equality gate and column-copy helper.
 * It does not run a numeric spline or the outer application's GUI callbacks. */
public class NumericCopyOracle {
 public static void main(String[] args)throws Exception {
  Class<?> f=Class.forName("tQa"),s=Class.forName("Rsa");
  Method index=Arrays.stream(f.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==int.class&&Modifier.isStatic(x.getModifiers())&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{String.class})).findFirst().orElseThrow();
  Field raw=f.getDeclaredField("X");raw.setAccessible(true);
  Method gate=Arrays.stream(s.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==boolean.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class,int.class})).findFirst().orElseThrow();
  Method copy=Arrays.stream(s.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==void.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class,f})).findFirst().orElseThrow();
  Method first=Arrays.stream(s.getMethods()).filter(x->x.getName().equals("h")&&x.getReturnType()==f&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class})).findFirst().orElseThrow();
  Method reference=Arrays.stream(s.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==f&&x.getParameterCount()==0).findFirst().orElseThrow();
  String[] properties={"Exposure2012","Sharpness","Exposure2012","Exposure2012","Exposure2012","Exposure2012","Exposure2012","CropLeft","CropLeft","Tint","Exposure2012","Exposure2012","Exposure2012","Exposure2012"};
  double[][] inputs={{0,99,-7},{1,999,2},{99,1,-2},{99,0,99,1,99},{99,0,99},{1e100,0,1e100},{1e100,0,-1e100},{.8,-.3,.5},{.8,-.3,.5,-.3,.8},{0,999,1},{-0.0,1,0.0},{-.12345678912345678,1,-.12345678912345678},{-0.0,1,2},{0,1e-15,2}};
  int[][] ratings={{0,4,0},{0,4,0},{0,0,0},{4,0,4,0,4},{4,0,4},{4,0,4},{4,0,4},{4,5,4},{4,5,0,5,4},{4,0,0},{4,0,4},{4,0,4},{4,0,0},{0,4,0}};
  System.out.print("{\"cases\":[");String sep="";
  for(int c=0;c<inputs.length;c++) {
   String property=args.length>0?args[0]:properties[c];int col=(int)index.invoke(null,property);
   if(col<0||col>=4096)throw new IllegalArgumentException(property);
   @SuppressWarnings("unchecked") List<Object> sequence=(List<Object>)s.getConstructor(int.class).newInstance(-1);
   int selected=4;for(int rating:ratings[c])if(property.startsWith("Crop")&&rating==5)selected=5;
   for(int i=0;i<inputs[c].length;i++) {
    Object[] data=new Object[4096];
    if(property.equals("Sharpness")||property.equals("Tint"))data[col]=Integer.valueOf((int)inputs[c][i]);else data[col]=Double.valueOf(inputs[c][i]);
    Object frame=f.getConstructor(Object[].class).newInstance((Object)data);f.getMethod("E",int.class).invoke(frame,ratings[c][i]);sequence.add(frame);
   }
   boolean copied=(boolean)gate.invoke(sequence,col,selected);
   Object source=first.invoke(sequence,selected);if(source==null)source=reference.invoke(sequence);
   if(copied)copy.invoke(sequence,col,source);
   List<Object> expected=new ArrayList<>();for(Object frame:sequence)expected.add(((Object[])raw.get(frame))[col]);
   System.out.print(sep+"{\"property\":\""+property+"\",\"input\":"+Arrays.toString(inputs[c])+",\"ratings\":"+Arrays.toString(ratings[c])+",\"copied\":"+copied+",\"expected\":"+expected+"}");sep=",";
  }
  System.out.println("]}");
 }
}
