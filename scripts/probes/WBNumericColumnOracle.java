package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;

/** Independent caller of the installed numeric column component. Mode zero
 * uses rating-2 anchors and fixed zero spline tension without reading GUI
 * controls. The fixture records the equivalent anchor indices and Go
 * smoothness=100. This is not the outer automatic workflow. */
public class WBNumericColumnOracle {
 public static void main(String[] args)throws Exception {
  Class<?> f=Class.forName("tQa"),s=Class.forName("Rsa"),m=Class.forName("lrtimelapse.LRTimelapseMain");
  Object main=m.getConstructor().newInstance();
  Method index=Arrays.stream(f.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==int.class&&Modifier.isStatic(x.getModifiers())&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{String.class})).findFirst().orElseThrow();
  Field raw=f.getDeclaredField("X");raw.setAccessible(true);
  Method transition=m.getMethod("A",s,int.class,int.class,boolean.class);
  Class<?> registryType=Class.forName("Ooa");
  Object registry=Arrays.stream(registryType.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==registryType&&Modifier.isStatic(x.getModifiers())&&x.getParameterCount()==0).findFirst().orElseThrow().invoke(null);
  Method valueType=Arrays.stream(registryType.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==Class.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{int.class})).findFirst().orElseThrow();
  int[][] keys={{0,2,4},{1,3},{0,1,3,4},{0,2,4}};
  System.out.print("{\"smoothness\":100,\"cases\":[");String sep="";
  for(String property:new String[]{"Temperature","Tint","IncrementalTemperature","IncrementalTint"}) {
   int col=(int)index.invoke(null,property);
   Class<?> registeredType=(Class<?>)valueType.invoke(registry,col);
   boolean integer=registeredType==Integer.class;
   if(!integer&&registeredType!=Double.class)throw new IllegalStateException(registeredType.toString());
   double a=property.equals("Temperature")?3000:integer?10:.123456789123;
   double b=property.equals("Temperature")?7000:integer?30:.789123456789;
   Double[][] cases={{a,null,null,b,null},{null,a,null,b,null},{a,null,b,null,a},{a,a,null,a,a}};
   if(col<0||col>=4096)throw new IllegalArgumentException(property);
   for(int c=0;c<cases.length;c++) {
    @SuppressWarnings("unchecked") List<Object> sequence=(List<Object>)s.getConstructor(int.class).newInstance(-1);
    for(int i=0;i<cases[c].length;i++) {
     Object[] data=new Object[4096];Double v=cases[c][i];
     if(v!=null){if(integer)data[col]=Integer.valueOf(v.intValue());else data[col]=v;}
     Object frame=f.getConstructor(Object[].class).newInstance((Object)data);
     for(int key:keys[c])if(i==key)f.getMethod("E",int.class).invoke(frame,2);
     sequence.add(frame);
    }
    transition.invoke(main,sequence,0,col,true);
    List<Object> expected=new ArrayList<>();for(Object frame:sequence)expected.add(((Object[])raw.get(frame))[col]);
    System.out.print(sep+"{\"property\":\""+property+"\",\"integer\":"+integer+",\"input\":"+Arrays.toString(cases[c])+",\"keys\":"+Arrays.toString(keys[c])+",\"expected\":"+expected+"}");sep=",";
   }
  }
  System.out.println("]}");
 }
}
