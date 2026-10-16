package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;

/** Independent caller of the installed numeric missing-value preflight. */
public class NumericMissingOracle {
 public static void main(String[]args)throws Exception {
  Class<?> f=Class.forName("tQa"),s=Class.forName("Rsa"),m=Class.forName("lrtimelapse.LRTimelapseMain");
  Object main=m.getConstructor().newInstance();
  Method fill=Arrays.stream(m.getDeclaredMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==boolean.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{s,int.class})).findFirst().orElseThrow();fill.setAccessible(true);
  Method index=Arrays.stream(f.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==int.class&&Modifier.isStatic(x.getModifiers())&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{String.class})).findFirst().orElseThrow();
  Field raw=f.getDeclaredField("X");raw.setAccessible(true);
  Method name=Arrays.stream(f.getMethods()).filter(x->x.getName().equals("K")&&x.getReturnType()==String.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class})).findFirst().orElseThrow();
  System.out.print("{\"excluded\":[");String sep="";
  for(String field:new String[]{"d","Ha","Wa","la","Z","Xa","ua","W"}){Field v=f.getDeclaredField(field);v.setAccessible(true);System.out.print(sep+"\""+name.invoke(null,v.getInt(null))+"\"");sep=",";}
  Double[][] inputs={{null,1.25,null,2.5,null},{null,null,-.123456789123,null,null},{0.0,null,null,-1.5},{null,null,null},{1.0,2.0,3.0},{null,0.0,null,0.0},{5.0,null,null,null},{null,null,null,5.0},{1.0,null,-2.0,null,3.0,null}};
  System.out.print("],\"cases\":[");sep="";
  int col=(int)index.invoke(null,"Exposure2012");
  for(Double[] input:inputs){
   @SuppressWarnings("unchecked") List<Object> sequence=(List<Object>)s.getConstructor(int.class).newInstance(-1);
   for(Double value:input){Object[] data=new Object[4096];data[col]=value;sequence.add(f.getConstructor(Object[].class).newInstance((Object)data));}
   boolean changed=(boolean)fill.invoke(main,sequence,col);
   List<Object> output=new ArrayList<>();for(Object frame:sequence)output.add(((Object[])raw.get(frame))[col]);
   System.out.print(sep+"{\"input\":"+Arrays.toString(input)+",\"expected\":"+output+",\"changed\":"+changed+"}");sep=",";
  }
  System.out.println("]}");
 }
}
