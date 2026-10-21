package lrtlprobe;
import java.nio.file.*;import java.util.*;
public class AtanOracle {
 static long state=755;static long next(){state+=0x9e3779b97f4a7c15L;long z=state;z=(z^(z>>>30))*0xbf58476d1ce4e5b9L;z=(z^(z>>>27))*0x94d049bb133111ebL;return z^(z>>>31);}
 static String row(double y,double x){return "[\""+Long.toUnsignedString(Double.doubleToRawLongBits(y),16)+"\",\""+Long.toUnsignedString(Double.doubleToRawLongBits(x),16)+"\",\""+Long.toUnsignedString(Double.doubleToLongBits(Math.atan2(y,x)),16)+"\"]";}
 public static void main(String[]args)throws Exception{double[]v={0.,-0.,1.,-1.,Double.POSITIVE_INFINITY,Double.NEGATIVE_INFINITY,Double.NaN,Double.MIN_VALUE,-Double.MIN_VALUE,Double.MAX_VALUE,-Double.MAX_VALUE,0x1p-29,0x1p-66,0x1p66,.4375,2.4375};StringBuilder out=new StringBuilder("[\n");int n=0;for(double y:v)for(double x:v){if(n++>0)out.append(",\n");out.append(row(y,x));}for(int i=0;i<2048;i++){double y=Double.longBitsToDouble(next()),x=Double.longBitsToDouble(next());if(n++>0)out.append(",\n");out.append(row(y,x));}Files.writeString(Path.of(args[0]),out.append("\n]\n"));System.out.println(n+" atan2 observations");}
}
