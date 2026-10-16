package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;
import sun.misc.Unsafe;

/** Measures the installed public frame-pair wrapper with actual string parsing. */
public class HolyGrailMetadataPairOracle {
 public static void main(String[] args)throws Exception {
  Field singleton=Unsafe.class.getDeclaredField("theUnsafe");singleton.setAccessible(true);Unsafe allocator=(Unsafe)singleton.get(null);
  Class<?> f=Class.forName("tQa"),c=Class.forName("lrtimelapse.cOM7"),w=Class.forName("Wd"),factory=Class.forName("ABA");
  Object controller=allocator.allocateInstance(c);
  Method empty=Arrays.stream(factory.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==w&&x.getParameterCount()==0).findFirst().orElseThrow();
  Method pair=Arrays.stream(c.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==double.class&&Arrays.equals(x.getParameterTypes(),new Class<?>[]{f,f})).findFirst().orElseThrow();
  StringBuilder json=new StringBuilder("{\"cases\":[");String sep="";
  for(String[] shutter:new String[][]{{"1/125","1/60"},{"0.3","0.4"},{"1","2"},{"1/100","1/200"}})for(String[] iso:new String[][]{{"100","125"},{"100.0","125.0"},{"0","125"}})for(String[] aperture:new String[][]{{"5.6","8"},{"8","5.6"},{"8.0","5.60"}}) {
   Object[] frames=new Object[2];
   for(int i=0;i<2;i++){frames[i]=f.getConstructor(Object[].class).newInstance((Object)new Object[4096]);field(f,"Aa").set(frames[i],empty.invoke(null));field(f,"l").set(frames[i],0.0);field(f,"sa").set(frames[i],shutter[i]);field(f,"S").set(frames[i],iso[i]);field(f,"oa").set(frames[i],aperture[i]);}
   double expected=(double)pair.invoke(controller,frames[0],frames[1]);
   if(!Double.isFinite(expected))throw new IllegalStateException("non-finite pair");
   json.append(sep).append("{\"before\":[\"").append(shutter[0]).append("\",\"").append(iso[0]).append("\",\"").append(aperture[0]).append("\"],\"after\":[\"").append(shutter[1]).append("\",\"").append(iso[1]).append("\",\"").append(aperture[1]).append("\"],\"expected\":").append(expected).append("}");sep=",";
  }
  System.out.println(json.append("]}"));
 }
 static Field field(Class<?> type,String name)throws Exception{Field f=type.getDeclaredField(name);f.setAccessible(true);return f;}
}
