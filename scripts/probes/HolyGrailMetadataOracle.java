package lrtlprobe;
import java.lang.reflect.*;
import java.util.*;

/** Independent calls to installed shutter/ISO and aperture string parsers. */
public class HolyGrailMetadataOracle {
 public static void main(String[] args)throws Exception {
  Class.forName("tQa");
  Class<?> c=Class.forName("lrtimelapse.tools.c");Method number=c.getDeclaredMethod("a",String.class),aperture=c.getDeclaredMethod("A",String.class);
  Field ns=Class.forName("nNa").getDeclaredField("a");ns.setAccessible(true);
  StringBuilder json=new StringBuilder("{\"namespace\":\"").append(ns.get(null)).append("\",\"fields\":[\"").append(decode("nUL","%p\u0003l\u0002}\u0004K\u0006}\u0013|")).append("\",\"").append(decode("OAa","ehC")).append("\",\"").append(decode("OAa","Z\\~^oYiI")).append("\"],\"number\":[");String sep="";
  String[] labels={"0","0.3","0.4","0.5","0.6","0.8","1","1.0","1.3","1.6","2","2.5","3","3.2","4","5","6","6.3","8","10","13","15","16","20","25","30","31","32","40","50","60","63","64","79","80","100","125","160","200","250","320","400","500","640","800","1000","1250","1600","2000","2500","3200","4000","5000","6400","8000","10000","12500","16000","20000","25000","32000","40000","50000","64000","80000","100000","123","100.0","100.5"};
  for(String text:labels) {
   double value=(double)number.invoke(null,text);
   json.append(sep).append("{\"input\":\"").append(text).append("\",\"expected\":").append(value).append("}");sep=",";
   if(!text.equals("0")) {String fraction="1/"+text;double v=(double)number.invoke(null,fraction);json.append(sep).append("{\"input\":\"").append(fraction).append("\",\"expected\":").append(v).append("}");}
  }
  json.append("],\"aperture\":[");sep="";
  for(String text:new String[]{"0","1","1.2","1.4","1.6","1.8","2","2.2","2.5","2.8","3.2","3.5","4","4.5","5","5.6","6.3","7.1","8","9","10","11","13","14","16","18","20","22","25","29","32","36","40","45","51","57","64","72","80","90","5.5","5.60","8.0","F8","f/8"}) {
   double value=(double)aperture.invoke(null,text);
   json.append(sep).append("{\"input\":\"").append(text).append("\",\"expected\":").append(value).append("}");sep=",";
  }
  System.out.println(json.append("]}"));
 }
 static String decode(String type,String value)throws Exception{if(type.equals("nUL"))type="de.schlichtherle.license.nUL";Method m=Class.forName(type).getDeclaredMethod("A",Object.class);m.setAccessible(true);return (String)m.invoke(null,value);}
}
