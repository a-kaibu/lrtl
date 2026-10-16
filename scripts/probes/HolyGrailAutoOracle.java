package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;
import sun.misc.Unsafe;

/** Independent grid driver using the installed numerical controller and its
 * stored raw/smoothed results. Does not run the UI worker/progress callbacks. */
public class HolyGrailAutoOracle {
 public static void main(String[] args)throws Exception {
  Field singleton=Unsafe.class.getDeclaredField("theUnsafe");singleton.setAccessible(true);Unsafe allocator=(Unsafe)singleton.get(null);
  Class<?> f=Class.forName("tQa"),s=Class.forName("Rsa"),c=Class.forName("lrtimelapse.cOM7"),t=Class.forName("lrtimelapse.com8"),w=Class.forName("Wd"),factory=Class.forName("ABA");
  Object controller=allocator.allocateInstance(c),table=allocator.allocateInstance(t);
  Field tableSequence=field(t,"M"),controllerTable=field(c,"wc");controllerTable.set(controller,table);
  Field preview=field(f,"O"),packet=field(f,"Aa"),shutter=field(f,"sa"),iso=field(f,"S"),aperture=field(f,"oa"),raw=field(f,"l"),smooth=field(f,"ga");
  Method empty=Arrays.stream(factory.getMethods()).filter(x->x.getName().equals("A")&&x.getReturnType()==w&&x.getParameterCount()==0).findFirst().orElseThrow();
  Method calculate=c.getMethod("a",s,float.class,float.class,float.class,float.class,boolean.class);
  if(field(Class.forName("cNa"),"f").getInt(null)!=0)throw new IllegalStateException("unexpected initial rotation");
  StringBuilder json=new StringBuilder("{\"cases\":[");String sep="";
  for(int count:new int[]{17,31,61})for(float reduce:new float[]{0,.35f}) {
   @SuppressWarnings("unchecked") List<Object> sequence=(List<Object>)s.getConstructor(int.class).newInstance(-1);
   double[] inputs=new double[count];int[] ratings=new int[count],isos=new int[count];
   for(int i=0;i<count;i++) {
    inputs[i]=((i*37%23)-11)*.003+(i%7)*.000125;ratings[i]=i%9==1?2:i%9==2?3:i%11==4||i%11==5?4:0;isos[i]=i%9>=2?125:100;
    Object frame=f.getConstructor(Object[].class).newInstance((Object)new Object[4096]);preview.set(frame,inputs[i]);packet.set(frame,empty.invoke(null));shutter.set(frame,"1");iso.set(frame,""+isos[i]);aperture.set(frame,"8");raw.set(frame,0.0);f.getMethod("E",int.class).invoke(frame,ratings[i]);sequence.add(frame);
   }
   tableSequence.set(table,sequence);
   float bestStretch=0,bestRotate=0;double stretchScore=Double.MAX_VALUE,rotateScore=Double.MAX_VALUE,maxMagnitude=0;double[] wide=new double[count];
   for(int step=1;step<=150;step++) {
    float candidate=step/50f-1.5f;calculate.invoke(controller,sequence,0f,candidate,0f,reduce,true);
    double score=0;for(Object frame:sequence){double value=(Double)raw.get(frame),fitted=(Double)smooth.get(frame);if(!Double.isFinite(value)||!Double.isFinite(fitted))throw new IllegalStateException("non-finite grid curve");maxMagnitude=Math.max(maxMagnitude,Math.abs(value));score+=Math.abs(value-fitted);}
    if(score<stretchScore){stretchScore=score;bestStretch=candidate;}
   }
   for(int step=1;step<=200;step++) {
    float candidate=step/25f-4f;calculate.invoke(controller,sequence,0f,bestStretch,candidate,reduce,true);
    if(step==1)for(int i=0;i<count;i++)wide[i]=(Double)raw.get(sequence.get(i));
    double score=0;for(Object frame:sequence){double value=(Double)raw.get(frame),fitted=(Double)smooth.get(frame);if(!Double.isFinite(value)||!Double.isFinite(fitted))throw new IllegalStateException("non-finite grid curve");maxMagnitude=Math.max(maxMagnitude,Math.abs(value));score+=Math.abs(fitted);}
    if(score<rotateScore){rotateScore=score;bestRotate=candidate;}
   }
   calculate.invoke(controller,sequence,0f,bestStretch,bestRotate,reduce,true);
   double[] expected=new double[count];for(int i=0;i<count;i++)expected[i]=(Double)raw.get(sequence.get(i));
   json.append(sep).append("{\"input\":").append(Arrays.toString(inputs)).append(",\"ratings\":").append(Arrays.toString(ratings)).append(",\"iso\":").append(Arrays.toString(isos)).append(",\"reduce\":").append((double)reduce).append(",\"stretch\":").append((double)bestStretch).append(",\"rotate\":").append((double)bestRotate).append(",\"stretchScore\":").append(stretchScore).append(",\"rotateScore\":").append(rotateScore).append(",\"maxMagnitude\":").append(maxMagnitude).append(",\"wideRotate\":").append((double)(1/25f-4f)).append(",\"wideExpected\":").append(Arrays.toString(wide)).append(",\"expected\":").append(Arrays.toString(expected)).append("}");sep=",";
  }
  System.out.println(json.append("]}"));
 }
 static Field field(Class<?> type,String name)throws Exception {Field f=type.getDeclaredField(name);f.setAccessible(true);return f;}
}
