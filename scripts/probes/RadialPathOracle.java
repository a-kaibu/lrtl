package lrtlprobe;
import java.lang.reflect.*;import java.nio.file.*;import java.util.*;
public class RadialPathOracle {
 static Method m(Class<?>c,String n,Class<?>r,Class<?>...p){Method v=Arrays.stream(c.getDeclaredMethods()).filter(x->x.getName().equals(n)&&x.getReturnType()==r&&Arrays.equals(x.getParameterTypes(),p)).findFirst().orElseThrow();v.setAccessible(true);return v;}
 static double[][] values(Class<?>f,List<Object>seq,String prefix,String ns,String[]props)throws Exception{double[][] out=new double[seq.size()][props.length];for(int i=0;i<seq.size();i++)for(int j=0;j<props.length;j++){Object v=m(f,"a",Object.class,String.class).invoke(seq.get(i),prefix+ns+props[j]);out[i][j]=v==null?Double.NaN:((Number)v).doubleValue();}return out;}
 static String json(double[][]v){return Arrays.deepToString(v).replace("NaN","null");}
 public static void main(String[]args)throws Exception{
 Class<?>f=Class.forName("tQa"),s=Class.forName("Rsa"),rad=Class.forName("hSa"),wd=Class.forName("Wd"),factory=Class.forName("ABA"),st=Class.forName("lrtimelapse.settings.LRTSettings");Object settings=m(st,"a",st).invoke(null);st.getField("_debug").setBoolean(settings,false);
 Field packet=f.getDeclaredField("Aa"),flag=f.getDeclaredField("G");packet.setAccessible(true);flag.setAccessible(true);
 String[]props={"Top","Bottom","Left","Right","Angle"},helpers={"CX","CY","RX","RY","AN"};StringBuilder out=new StringBuilder("[\n");int count=0;
 for(int mode:new int[]{1,2})for(int slot:new int[]{0,1})for(int[]dims:new int[][]{{6000,4000},{4000,6000},{1000,1000}})for(int pattern=0;pattern<10;pattern++){
  @SuppressWarnings("unchecked")List<Object>seq=(List<Object>)s.getConstructor(int.class).newInstance(-1);String nativePrefix=null,prefix=null;double[][] angles={{-70,0,70},{42,48,52},{-88,88,85},{12,13,11},{0,85,80},{0,0,0},{45,-45,135},{-225,-135,225},{12,12,12},{0,3,6}};
  for(int frame=0;frame<7;frame++){
   Object item=f.getConstructor(Object[].class).newInstance((Object)new Object[4096]);String xml="<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description xmlns:crs=\"http://ns.adobe.com/camera-raw-settings/1.0/\">"+(mode==1?"<crs:GradientBasedCorrections><rdf:Seq/></crs:GradientBasedCorrections>":"")+"</rdf:Description></rdf:RDF></x:xmpmeta>";
   packet.set(item,m(factory,"A",wd,String.class).invoke(null,xml));flag.setInt(item,frame%3==0?1:0);
   String observed=((String[])m(rad,"A",String[].class,f).invoke(null,item))[slot];if(nativePrefix==null){nativePrefix=observed;prefix=observed.substring(0,observed.length()-1)+"/";}else if(!nativePrefix.equals(observed))throw new Exception("unstable layout");
   m(f,"a",void.class,int.class,Object.class).invoke(item,1,dims[0]);m(f,"a",void.class,int.class,Object.class).invoke(item,2,dims[1]);
   int a=Math.min(frame/3,2);double[]v={.15+a*.04,.75+a*.01,.2-a*.03,.8+a*.06,angles[pattern][a]};
   if(pattern==3){v[0]=.1;v[1]=.8;v[2]=.2;v[3]=.8+(a-1)*.001;}if(pattern==5){v[0]=.8;v[1]=.2;v[2]=.9;v[3]=.1;}if(pattern==8){v[0]=v[1]=v[2]=v[3]=.5;}if(pattern==9){v[0]=v[2]=.2;v[1]=v[3]=.8;}
   for(int j=0;j<5;j++){int index=(int)m(f,"A",int.class,String.class).invoke(null,prefix+"crs:"+props[j]);if(index<0)throw new Exception("unregistered corrected path "+prefix);m(f,"a",void.class,int.class,Object.class).invoke(item,index,v[j]);}
   seq.add(item);
  }
  double[][]input=values(f,seq,prefix,"crs:",props);m(rad,"a",void.class,s).invoke(null,seq);double[][]pre=values(f,seq,prefix,"crs:",props),preAux=values(f,seq,prefix,"lrt:",helpers);Field df=rad.getDeclaredField("D");df.setAccessible(true);double offset=((double[])df.get(null))[slot];
  for(Object item:seq)for(int j=0;j<5;j++)m(f,"A",void.class,String.class,Object.class).invoke(item,prefix+"lrt:"+helpers[j],new double[]{.4,.5,1800,1200,10}[j]);
  double[][]given=values(f,seq,prefix,"lrt:",helpers);m(rad,"A",void.class,s).invoke(null,seq);double[][]post=values(f,seq,prefix,"crs:",props),postAux=values(f,seq,prefix,"lrt:",helpers);
  if(count++>0)out.append(",\n");out.append("{\"Mode\":").append(mode).append(",\"Slot\":").append(slot+1).append(",\"Width\":").append(dims[0]).append(",\"Height\":").append(dims[1]).append(",\"Pattern\":").append(pattern).append(",\"NativePrefix\":\"").append(nativePrefix).append("\",\"RegisteredPrefix\":\"").append(prefix).append("\",\"NativeAngleIndex\":").append(m(f,"A",int.class,String.class).invoke(null,nativePrefix+"crs:Angle")).append(",\"RegisteredAngleIndex\":").append(m(f,"A",int.class,String.class).invoke(null,prefix+"crs:Angle")).append(",\"Offset\":").append(offset).append(",\"Input\":").append(json(input)).append(",\"Prepared\":").append(json(pre)).append(",\"PreparedHelpers\":").append(json(preAux)).append(",\"GivenHelpers\":").append(json(given)).append(",\"Restored\":").append(json(post)).append(",\"RestoredHelpers\":").append(json(postAux)).append("}");
 }Files.writeString(Path.of(args[0]),out.append("\n]\n"));System.out.println("Observed radial path cases: "+count);
 }
}
