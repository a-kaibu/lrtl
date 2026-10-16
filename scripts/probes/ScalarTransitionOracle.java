package lrtlprobe;

import java.lang.reflect.*;
import java.util.*;

/** Independently authored caller of installed scalar transition helpers.
 * Run with the user's LRT installation on the classpath. No product code is
 * embedded here; output consists only of synthetic inputs and measurements.
 */
public class ScalarTransitionOracle {
 public static void main(String[] args) throws Exception {
  Class<?> frameType=Class.forName("tQa"), sequenceType=Class.forName("Rsa");
  Class<?> mainType=Class.forName("lrtimelapse.LRTimelapseMain");
  Object main=mainType.getConstructor().newInstance();
  Method fill=Arrays.stream(mainType.getDeclaredMethods()).filter(m->m.getName().equals("A")&&m.getReturnType()==boolean.class&&Arrays.equals(m.getParameterTypes(),new Class<?>[]{sequenceType,int.class})).findFirst().orElseThrow();
  fill.setAccessible(true);
  Method set=frameType.getMethod("a",int.class,Object.class);
  Field raw=frameType.getDeclaredField("X");raw.setAccessible(true);
  Method index=Arrays.stream(frameType.getMethods()).filter(m->m.getName().equals("A")&&m.getReturnType()==int.class&&Modifier.isStatic(m.getModifiers())&&Arrays.equals(m.getParameterTypes(),new Class<?>[]{String.class})).findFirst().orElseThrow();
  Method transition=mainType.getMethod("A",sequenceType,int.class,int.class,boolean.class);
  Method equal=Arrays.stream(sequenceType.getMethods()).filter(m->m.getName().equals("A")&&m.getReturnType()==boolean.class&&Arrays.equals(m.getParameterTypes(),new Class<?>[]{int.class})).findFirst().orElseThrow();
  Object[][] inputs={
   {null,true,null,false,null}, {null,null,true,null,null},
   {false,null,null,true,null,false}, {null,null,null},
   {"",null,"Custom",null,"As Shot"}, {null," ",null,"Custom",null},
   {null,"Camera Neutral",null,"Adobe Color",null},
   {"Custom","CUSTOM","custom"}, {null,"",null,""}, {null,null,null},
   {"I","\u0131","i"}, {"\u0130","i","I"}
  };
  System.out.print("{\"cases\":[");String separator="";
  for(int c=0;c<inputs.length;c++) {
   String property=c<4?"HasCrop":c==6?"crs:CameraProfile":"WhiteBalance";
   int column=(int)index.invoke(null,property);
   if(column<0)throw new IllegalStateException("Unknown property "+property);
   @SuppressWarnings("unchecked") List<Object> sequence=(List<Object>)sequenceType.getConstructor(int.class).newInstance(-1);
   for(Object value:inputs[c]) {
    Object frame=frameType.getConstructor(Object[].class).newInstance((Object)new Object[4096]);
    set.invoke(frame,column,value);sequence.add(frame);
   }
   boolean changed=(boolean)fill.invoke(main,sequence,column);
   Object[] filled=new Object[sequence.size()];
   for(int i=0;i<filled.length;i++)filled[i]=((Object[])raw.get(sequence.get(i)))[column];
   boolean skipped=(boolean)equal.invoke(sequence,column);
   if(c>=4&&!skipped)transition.invoke(main,sequence,4,column,true);
   Object[] expected=new Object[sequence.size()];
   for(int i=0;i<expected.length;i++)expected[i]=((Object[])raw.get(sequence.get(i)))[column];
   System.out.print(separator+"{\"property\":"+json(property)+",\"input\":"+array(inputs[c])+",\"filled\":"+array(filled)+",\"changed\":"+changed+",\"expected\":"+array(expected)+"}");separator=",";
  }
  System.out.println("]}");
 }
 static String array(Object[] values){StringJoiner j=new StringJoiner(",","[","]");for(Object v:values)j.add(json(v));return j.toString();}
 static String json(Object v){
  if(v==null)return "null";if(v instanceof Boolean)return v.toString();
  StringBuilder out=new StringBuilder("\"");
  for(char ch:v.toString().toCharArray()){
   if(ch<32||ch>126)out.append(String.format("\\u%04x",(int)ch));
   else {if(ch=='\\'||ch=='\"')out.append('\\');out.append(ch);}
  }
  return out.append('"').toString();
 }
}
