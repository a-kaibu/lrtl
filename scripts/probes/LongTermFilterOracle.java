package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;

// Synthetic observations of native curve helpers and public preset defaults.
// The application UI worker is not invoked by this harness.
public final class LongTermFilterOracle {
    static Method method(Class<?> c, String name, Class<?> result, Class<?>... p) {
        Method m = Arrays.stream(c.getDeclaredMethods()).filter(x -> x.getName().equals(name)
            && x.getReturnType() == result && Arrays.equals(x.getParameterTypes(),p))
            .findFirst().orElseThrow();
        m.setAccessible(true); return m;
    }
    static String numbers(double[] values) {
        StringJoiner out = new StringJoiner(",","[","]");
        for(double v:values) out.add(Double.isFinite(v)?Double.toString(v):"null");
        return out.toString();
    }
    public static void main(String[] args) throws Exception {
        Class.forName("tQa");
        Class<?> helper = Class.forName("lrtimelapse.eA");
        Method constant = method(helper,"A",double[].class,double[].class,double.class);
        Method line = method(helper,"a",double[].class,double[].class);
        Method smooth = method(helper,"A",double[].class,int.class,int[].class,double[].class,double.class);
        Class<?> presetType = Class.forName("lrtimelapse.settings.FilterPreset");
        Object defaults = presetType.getConstructor().newInstance();
        StringJoiner fields = new StringJoiner(",","{","}");
        Field[] publicFields = presetType.getFields();
        Arrays.sort(publicFields,Comparator.comparing(Field::getName));
        for(Field f:publicFields) if(f.getType()==int.class || f.getType()==boolean.class)
            fields.add("\""+f.getName()+"\":"+f.get(defaults));
        String defaultKey = (String)method(Class.forName("ps"),"A",String.class,Object.class)
            .invoke(null,"\u0000\u001c\u0002\u0018\u0011\u0015\u0010");
        String dateFormat = (String)method(Class.forName("ps"),"A",String.class,Object.class)
            .invoke(null,"\u001d\u0000\u001d\u0000I4)T\u0000\u001dD1,C\t\u0014^\n\u0017");
        String missingDate = (String)method(Class.forName("eJ"),"A",String.class,Object.class)
            .invoke(null,"\u001d9\u0012");
        StringJoiner thresholds = new StringJoiner(",","[","]");
        for(int slider=0;slider<=100;slider++) {
            float lower=(-1f*(2f-(float)slider/50f))/10f;
            float upper=(2f-(float)(100-slider)/50f)/10f;
            thresholds.add("[\""+Integer.toHexString(Float.floatToRawIntBits(lower))+"\",\""+Integer.toHexString(Float.floatToRawIntBits(upper))+"\"]");
        }
        StringBuilder out = new StringBuilder("{\"ThresholdBits\":").append(thresholds).append(",\"DefaultKey\":\"").append(defaultKey)
            .append("\",\"DateFormat\":\"").append(dateFormat)
            .append("\",\"MissingDate\":\"").append(missingDate)
            .append("\",\"Defaults\":").append(fields).append(",\"Rows\":[\n");
        int cases = 0;
        for(int count:new int[]{1,2,5,7,8,12,33}) for(int stride:new int[]{1,3})
        for(int pattern=0;pattern<6;pattern++) for(int mode=0;mode<2;mode++) {
            int[] settings = mode==0 ? new int[]{0,1,5,17,33,50}:new int[]{-100,-30,0,40,100};
            for(int setting:settings) {
                TreeSet<Integer> indices = new TreeSet<>();
                for(int i=0;i<count;i+=stride) indices.add(i);
                indices.add(count-1);
                int[] selected=indices.stream().mapToInt(Integer::intValue).toArray();
                double[] input=new double[selected.length];
                for(int k=0;k<input.length;k++) {
                    int i=selected[k];
                    switch(pattern) {
                        case 0: input[k]=.4;break;
                        case 1: input[k]=.1+.8*i/Math.max(1.,count-1);break;
                        case 2: input[k]=i==count/2?.95:.3;break;
                        case 3: input[k]=i%2==0?.2:.8;break;
                        case 4: input[k]=i<count/2?.98:.02;break;
                        default:input[k]=.4+.15*Math.sin(i*.7)+(i%5==1?.2:0);break;
                    }
                }
                boolean active=mode==1 || setting>0;
                double[] target;
                if(!active) target=new double[0];
                else if(mode==1) target=(double[])constant.invoke(null,input.clone(),setting/150.);
                else if(input.length<8) target=(double[])line.invoke(null,(Object)input.clone());
                else target=(double[])smooth.invoke(null,count,selected,input.clone(),setting/80.);
                if(cases++>0)out.append(",\n");
                out.append("{\"Count\":").append(count).append(",\"Indices\":").append(Arrays.toString(selected))
                    .append(",\"Pattern\":").append(pattern).append(",\"Constant\":").append(mode==1)
                    .append(",\"Setting\":").append(setting).append(",\"Input\":").append(numbers(input))
                    .append(",\"Target\":").append(numbers(target)).append("}");
            }
        }
        Files.writeString(Path.of(args[0]),out.append("\n]}\n"));
        System.out.println("Observed native long-term curves: "+cases);
    }
}
