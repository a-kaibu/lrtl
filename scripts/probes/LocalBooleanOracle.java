package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;

/** Independent calls to registered boolean preflight, gate and copy helpers. */
public class LocalBooleanOracle {
 public static void main(String[] args)throws Exception {
  Class<?> f=Class.forName("tQa"),s=Class.forName("Rsa"),m=Class.forName("lrtimelapse.LRTimelapseMain"),r=Class.forName("Ooa");
  Object main=m.getConstructor().newInstance();
  Method index=Arrays.stream(f.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==int.class&&Modifier.isStatic(x.getModifiers())&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{String.class})).findFirst().orElseThrow();
  Object registry=Arrays.stream(r.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==r&&Modifier.isStatic(x.getModifiers())&&x.getParameterCount()==0).findFirst().orElseThrow().invoke(null);
  Method type=Arrays.stream(r.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==Class.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class})).findFirst().orElseThrow();
  Method fill=Arrays.stream(m.getDeclaredMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==boolean.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{s,int.class})).findFirst().orElseThrow();fill.setAccessible(true);
  Method gate=Arrays.stream(s.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==boolean.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class,int.class})).findFirst().orElseThrow();
  Method copy=Arrays.stream(s.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==void.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class,f})).findFirst().orElseThrow();
  Method first=Arrays.stream(s.getMethods()).filter(x->x.getName().equals("h")&&x.getReturnType()==f&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class})).findFirst().orElseThrow();
  Method reference=Arrays.stream(s.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==f&&x.getParameterCount()==0).findFirst().orElseThrow();
  Field raw=f.getDeclaredField("X");raw.setAccessible(true);
  List<String> registered=new ArrayList<>();
  for(String collection:new String[]{"GradientBasedCorrections","CircularGradientBasedCorrections","PaintBasedCorrections","MaskGroupBasedCorrections"})for(int position=1;position<=12;position++)for(String suffix:new String[]{"/crs:CorrectionActive","/crs:CorrectionMasks[1]/crs:Flipped","/crs:CorrectionMasks[1]/crs:MaskInverted"}) {
   String path=collection+"["+position+"]"+suffix;int col=(int)index.invoke(null,path);
   if(col>=0&&type.invoke(registry,col)==Boolean.class)registered.add(path);
  }
  System.out.print("{\"registered\":[");String sep="";for(String path:registered){System.out.print(sep+"\""+path+"\"");sep=",";}
  System.out.print("],\"cases\":[");sep="";
  Boolean[][] inputs={{false,true,false},{false,true,true},{false,true,false},{null,true,null,false,null},{null,null,null},{true,null,false},{true,null,false},{false,false,false}};
  int[][] ratings={{4,0,4},{4,0,4},{0,4,0},{4,0,4,0,4},{4,0,4},{5,5,5},{4,4,4},{4,4,4}};
  for(String path:new String[]{"GradientBasedCorrections[1]/crs:CorrectionActive","CircularGradientBasedCorrections[1]/crs:CorrectionMasks[1]/crs:Flipped","MaskGroupBasedCorrections[1]/crs:CorrectionMasks[1]/crs:Flipped"})for(int c=0;c<inputs.length;c++) {
   int col=(int)index.invoke(null,path);
   @SuppressWarnings("unchecked") List<Object> sequence=(List<Object>)s.getConstructor(int.class).newInstance(-1);
   for(int i=0;i<inputs[c].length;i++){Object[] data=new Object[4096];data[col]=inputs[c][i];Object frame=f.getConstructor(Object[].class).newInstance((Object)data);f.getMethod("E",int.class).invoke(frame,ratings[c][i]);sequence.add(frame);}
   fill.invoke(main,sequence,col);
   if(Arrays.stream(inputs[c]).anyMatch(Objects::nonNull)&&(boolean)gate.invoke(sequence,col,4)) {
    Object source=first.invoke(sequence,4);if(source==null)source=reference.invoke(sequence);copy.invoke(sequence,col,source);
   }
   List<Object> expected=new ArrayList<>();for(Object frame:sequence)expected.add(((Object[])raw.get(frame))[col]);
   System.out.print(sep+"{\"property\":\""+path+"\",\"input\":"+Arrays.toString(inputs[c])+",\"ratings\":"+Arrays.toString(ratings[c])+",\"expected\":"+expected+"}");sep=",";
  }
  System.out.println("]}");
 }
}
