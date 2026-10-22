package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;
import java.util.concurrent.ConcurrentSkipListSet;
import sun.misc.Unsafe;

// The subclass suppresses only a status/progress callback; date selection
// runs in the unchanged native worker. No windows or preferences are created.
public final class LongTermTimeOracle {
    public static class QuietTable extends lrtimelapse.cOM7 {
        public QuietTable() { super(null); }
        @Override public void K(String title,String detail,int position,int count) {}
    }
    static Method method(Class<?> c,String name,Class<?> result,Class<?>... p) {
        Method m=Arrays.stream(c.getDeclaredMethods()).filter(x->x.getName().equals(name)
            && x.getReturnType()==result && Arrays.equals(x.getParameterTypes(),p)).findFirst().orElseThrow();
        m.setAccessible(true);return m;
    }
    static void set(Object object,Class<?> owner,String name,Object value) throws Exception {
        Field f=owner.getDeclaredField(name);f.setAccessible(true);f.set(object,value);
    }
    public static void main(String[] args) throws Exception {
        TimeZone.setDefault(TimeZone.getTimeZone("Asia/Tokyo"));
        Class<?> frame=Class.forName("tQa"),sequence=Class.forName("Rsa"),panel=Class.forName("Dra");
        Class<?> table=Class.forName("lrtimelapse.cOM7"),model=Class.forName("lrtimelapse.com8");
        Field field=Unsafe.class.getDeclaredField("theUnsafe");field.setAccessible(true);Unsafe unsafe=(Unsafe)field.get(null);
        int dateColumn=frame.getDeclaredField("Xa").getInt(null);
        String[] names={"ca","ha","q","Ha","Y","z","W"};
        StringBuilder out=new StringBuilder("[\n");int cases=0;
        for(int config=0;config<9;config++)for(int missing=0;missing<4;missing++) {
            List<String> dates=new ArrayList<>();
            for(int day=19;day<=25;day++)for(String clock:new String[]{"00:00:00","08:00:59","08:01:00","12:00:00","17:59:59","18:00:00","23:59:59"})dates.add("2026-10-"+day+"T"+clock);
            dates.add("not a date");dates.add("2026-02-30T12:00:00");dates.add("2026-10-19T12:00:00+09:00");
            if(missing>0)dates.set(5,missing==1?"":missing==2?"n/a":null);
            @SuppressWarnings("unchecked")List<Object> frames=(List<Object>)sequence.getConstructor(int.class).newInstance(-1);
            for(String date:dates) {Object f=frame.getConstructor(Object[].class).newInstance((Object)new Object[4096]);method(frame,"a",void.class,int.class,Object.class).invoke(f,dateColumn,date);frames.add(f);}
            Object view=unsafe.allocateInstance(QuietTable.class),data=unsafe.allocateInstance(model),filter=unsafe.allocateInstance(panel);
            set(view,table,"wc",data);set(data,model,"M",frames);set(filter,panel,"L",view);
            boolean available=(boolean)method(sequence,"Y",boolean.class).invoke(frames);set(filter,panel,"y",available);
            int start=config==0||config==7?0:config==4?22:config==5?12:8;
            int end=config==0||config==7?24:config==4?6:config==5?12:18;
            set(filter,panel,"n",start);set(filter,panel,"m",0);set(filter,panel,"Z",end);set(filter,panel,"H",0);
            boolean[] days=new boolean[7];
            for(int i=0;i<7;i++) {days[i]=config==1||config==4||config==5||config==8||config==0||(config==2&&i==0)||(config==3&&i>=5)||(config==7&&i!=0);set(filter,panel,names[i],days[i]);}
            ConcurrentSkipListSet<Integer> selected=new ConcurrentSkipListSet<>();for(int i=0;i<frames.size();i++)selected.add(i);
            Object progress=Class.forName("ssa").getConstructors()[0].newInstance(filter,"probe",null,false,new ConcurrentSkipListSet<Integer>());
            boolean shortcut=(boolean)method(panel,"A",boolean.class).invoke(filter);
            if(available && !shortcut)method(panel,"a",void.class,Set.class,Class.forName("lrtimelapse.tools.Prn")).invoke(filter,selected,progress);
            if(cases++>0)out.append(",\n");
            out.append("{\"Start\":").append(start*60).append(",\"End\":").append(end*60).append(",\"Days\":").append(Arrays.toString(days))
                .append(",\"Available\":").append(available).append(",\"Shortcut\":").append(shortcut).append(",\"Dates\":[");
            for(int i=0;i<dates.size();i++) {if(i>0)out.append(',');String date=dates.get(i);out.append(date==null?"null":"\""+date+"\"");}
            out.append("],\"Selected\":").append(selected.toString()).append('}');
        }
        Files.writeString(Path.of(args[0]),out.append("\n]\n"));
        System.out.println("Observed native time filters: "+cases);
        System.exit(0);
    }
}
