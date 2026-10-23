package lrtlprobe;

import java.lang.reflect.*;
import java.nio.file.*;
import java.util.*;

// Original synthetic measurement harness for the two native XML helpers used
// by full metadata paste. It does not invoke clipboard/UI or save preferences.
public final class MetadataPasteOracle {
    static Method method(Class<?> c,String name,Class<?> result,Class<?>... p) {
        Method m=Arrays.stream(c.getDeclaredMethods()).filter(x->x.getName().equals(name)
            &&x.getReturnType()==result&&Arrays.equals(x.getParameterTypes(),p)).findFirst().orElseThrow();
        m.setAccessible(true);return m;
    }
    static String quote(String text) {
        StringBuilder out=new StringBuilder("\"");
        for(char c:text.toCharArray()) {
            switch(c) {case '\\':out.append("\\\\");break;case '"':out.append("\\\"");break;
                case '\n':out.append("\\n");break;case '\r':out.append("\\r");break;case '\t':out.append("\\t");break;
                default:if(c<32)out.append(String.format("\\u%04x",(int)c));else out.append(c);}
        }
        return out.append('"').toString();
    }
    static String packet(String attrs,String body) {
        return "<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\"><rdf:Description rdf:about=\"\" xmlns:crs=\"http://ns.adobe.com/camera-raw-settings/1.0/\" xmlns:xmp=\"http://ns.adobe.com/xap/1.0/\" xmlns:exif=\"http://ns.adobe.com/exif/1.0/\" xmlns:tiff=\"http://ns.adobe.com/tiff/1.0/\" xmlns:dc=\"http://purl.org/dc/elements/1.1/\" xmlns:f=\"urn:lrtl:paste:test/\" xmlns:lrt=\"http://lrtimelapse.com/\" "+attrs+">"+body+"</rdf:Description></rdf:RDF></x:xmpmeta>";
    }
    static String array(String kind,String... items) {
        StringBuilder out=new StringBuilder("<rdf:"+kind+">");
        for(String item:items)out.append("<rdf:li>").append(item).append("</rdf:li>");
        return out.append("</rdf:"+kind+">").toString();
    }
    public static void main(String[] args)throws Exception {
        Class.forName("tQa");
        Class<?> xml=Class.forName("Wd"),factory=Class.forName("ABA"),options=Class.forName("bCA");
        Method parse=method(factory,"A",xml,String.class),serialize=method(factory,"A",String.class,xml,options);
        Method clear=method(Class.forName("xOa"),"A",void.class,xml,String.class);
        Method append=method(Class.forName("ieA"),"A",void.class,xml,xml,boolean.class,boolean.class);
        if(args.length==2&&args[0].equals("--roundtrip")) {
            int count=0;
            try(var paths=Files.list(Path.of(args[1]))) {
                for(Path path:paths.filter(p->p.getFileName().toString().endsWith(".input.xmp")).sorted().toList()) {
                    Object packet=parse.invoke(null,Files.readString(path));
                    String saved=(String)serialize.invoke(null,packet,options.getConstructor(int.class).newInstance(16));
                    Path output=path.resolveSibling(path.getFileName().toString().replace(".input.xmp",".native.xmp"));
                    Files.writeString(output,saved,StandardOpenOption.CREATE_NEW);
                    count++;
                }
            }
            System.out.println("ROUNDTRIPPED "+count+" XMP PACKETS");
            return;
        }
        List<String[]> cases=new ArrayList<>();
        cases.add(new String[]{"scalars",packet("crs:Exposure2012=\"0.75\" crs:Temperature=\"5500\" xmp:Rating=\"5\" exif:DateTimeOriginal=\"2026-10-01T01:00:00\" f:Added=\"source\"",""),packet("crs:Exposure2012=\"-0.25\" crs:Contrast2012=\"25\" xmp:Rating=\"4\" exif:DateTimeOriginal=\"2026-10-02T02:00:00\" f:Kept=\"target\"","")});
        cases.add(new String[]{"missing-capture",packet("xmp:Rating=\"5\" exif:DateTimeOriginal=\"2026-10-01T01:00:00\" tiff:Make=\"Synthetic\" lrt:ExternalExposureDefault=\"2.5\"",""),packet("crs:Exposure2012=\"1\" f:Kept=\"target\"","")});
        cases.add(new String[]{"empty-source",packet("",""),packet("crs:Exposure2012=\"1\" xmp:Rating=\"4\" f:Kept=\"target\"","")});
        cases.add(new String[]{"empty-target",packet("crs:Exposure2012=\".5\" xmp:Rating=\"5\"","<dc:subject>"+array("Bag","one","two")+"</dc:subject>"),packet("","")});
        cases.add(new String[]{"struct-union",packet("","<f:Struct><rdf:Description f:Common=\"source\" f:Added=\"new\"><f:Nested rdf:parseType=\"Resource\"><f:Source>yes</f:Source></f:Nested></rdf:Description></f:Struct>"),packet("","<f:Struct><rdf:Description f:Common=\"target\" f:Kept=\"old\"><f:Nested rdf:parseType=\"Resource\"><f:Target>yes</f:Target></f:Nested></rdf:Description></f:Struct>")});
        for(String kind:new String[]{"Bag","Seq","Alt"}) {
            cases.add(new String[]{"array-"+kind,packet("","<f:Array>"+array(kind,"same","new","new")+"</f:Array>"),packet("","<f:Array>"+array(kind,"old","same")+"</f:Array>")});
            cases.add(new String[]{"empty-array-"+kind,packet("","<f:Array>"+array(kind,"new")+"</f:Array>"),packet("","<f:Array>"+array(kind)+"</f:Array>")});
        }
        cases.add(new String[]{"language-alt",packet("","<dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">source default</rdf:li><rdf:li xml:lang=\"ja-JP\">日本語</rdf:li><rdf:li xml:lang=\"en-US\">source English</rdf:li></rdf:Alt></dc:description>"),packet("","<dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">target default</rdf:li><rdf:li xml:lang=\"en-US\">target English</rdf:li></rdf:Alt></dc:description>")});
        cases.add(new String[]{"struct-array-items",packet("","<f:Array><rdf:Bag><rdf:li><rdf:Description f:A=\"1\" f:B=\"2\"/></rdf:li><rdf:li><rdf:Description f:A=\"1\"/></rdf:li></rdf:Bag></f:Array>"),packet("","<f:Array><rdf:Bag><rdf:li><rdf:Description><f:B>2</f:B><f:A>1</f:A></rdf:Description></rdf:li></rdf:Bag></f:Array>")});
        cases.add(new String[]{"array-type-mismatch",packet("","<f:Array>"+array("Seq","source")+"</f:Array>"),packet("","<f:Array>"+array("Bag","target")+"</f:Array>")});
        cases.add(new String[]{"struct-scalar-mismatch",packet("","<f:Value><rdf:Description f:Added=\"yes\"/></f:Value>"),packet("f:Value=\"keep scalar\"","")});
        cases.add(new String[]{"empty-values",packet("f:Empty=\"\" f:Kept=\"source\"","<f:Array>"+array("Bag","","new")+"</f:Array>"),packet("f:Kept=\"\"","<f:Array>"+array("Bag","old","")+"</f:Array>")});
        cases.add(new String[]{"uri-values",packet("","<f:URI rdf:resource=\"urn:source\"/><f:Array><rdf:Bag><rdf:li rdf:resource=\"urn:same\"/><rdf:li>urn:same</rdf:li></rdf:Bag></f:Array>"),packet("","<f:URI rdf:resource=\"urn:target\"/><f:Array><rdf:Bag><rdf:li rdf:resource=\"urn:same\"/></rdf:Bag></f:Array>")});
        cases.add(new String[]{"qualified-values",packet("","<f:Value rdf:parseType=\"Resource\"><rdf:value>source</rdf:value><f:Unit>source unit</f:Unit></f:Value>"),packet("","<f:Value rdf:parseType=\"Resource\"><rdf:value>target</rdf:value><f:Unit>target unit</f:Unit></f:Value>")});
        cases.add(new String[]{"crs-structures",packet("crs:HasSettings=\"True\"","<crs:ToneCurvePV2012>"+array("Seq","0, 0","255, 240")+"</crs:ToneCurvePV2012><crs:MaskGroupBasedCorrections><rdf:Seq><rdf:li><rdf:Description crs:CorrectionSyncID=\"source-id\" crs:LocalExposure2012=\".5\"><crs:CorrectionMasks><rdf:Seq><rdf:li><rdf:Description crs:What=\"Mask/CircularGradient\" crs:Top=\".2\" crs:Bottom=\".8\"/></rdf:li></rdf:Seq></crs:CorrectionMasks></rdf:Description></rdf:li></rdf:Seq></crs:MaskGroupBasedCorrections>"),packet("crs:HasSettings=\"False\"","<crs:ToneCurvePV2012>"+array("Seq","0, 0","255, 255")+"</crs:ToneCurvePV2012><crs:UnknownTarget>remove</crs:UnknownTarget>")});
        cases.add(new String[]{"missing-empty-composites",packet("f:Empty=\"\"","<f:Struct rdf:parseType=\"Resource\"/><f:Array>"+array("Bag")+"</f:Array>"),packet("","")});
        cases.add(new String[]{"new-empty-array-item",packet("","<f:Array>"+array("Bag","","new")+"</f:Array>"),packet("","<f:Array>"+array("Bag","old")+"</f:Array>")});
        cases.add(new String[]{"empty-struct-field",packet("","<f:Struct><rdf:Description f:Empty=\"\" f:Added=\"new\"/></f:Struct>"),packet("","<f:Struct><rdf:Description f:Kept=\"old\"/></f:Struct>")});
        cases.add(new String[]{"language-default-insert",packet("","<dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">source default</rdf:li><rdf:li xml:lang=\"ja-jp\">Japanese</rdf:li></rdf:Alt></dc:description>"),packet("","<dc:description><rdf:Alt><rdf:li xml:lang=\"en-us\">target English</rdf:li></rdf:Alt></dc:description>")});
        cases.add(new String[]{"dc-bag-promotion",packet("dc:subject=\"source\"",""),packet("dc:subject=\"target\"","")});
        cases.add(new String[]{"dc-seq-promotion",packet("dc:creator=\"source\"",""),packet("dc:creator=\"target\"","")});
        cases.add(new String[]{"dc-alt-promotion",packet("dc:title=\"source\"",""),packet("dc:title=\"target\"","")});
        cases.add(new String[]{"nested-arrays",packet("","<f:Array><rdf:Bag><rdf:li>"+array("Bag","one","two")+"</rdf:li><rdf:li>"+array("Bag","one")+"</rdf:li></rdf:Bag></f:Array>"),packet("","<f:Array><rdf:Bag><rdf:li>"+array("Bag","two","one")+"</rdf:li></rdf:Bag></f:Array>")});
        cases.add(new String[]{"custom-qualified-array",packet("","<f:Array><rdf:Bag><rdf:li rdf:parseType=\"Resource\"><rdf:value>same</rdf:value><f:Unit>source unit</f:Unit></rdf:li></rdf:Bag></f:Array>"),packet("","<f:Array><rdf:Bag><rdf:li rdf:parseType=\"Resource\"><rdf:value>same</rdf:value><f:Unit>target unit</f:Unit></rdf:li></rdf:Bag></f:Array>")});
        cases.add(new String[]{"empty-alt-text-language",packet("","<dc:description><rdf:Alt><rdf:li xml:lang=\"ja-JP\"/></rdf:Alt></dc:description>"),packet("","<dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">target</rdf:li></rdf:Alt></dc:description>")});
        cases.add(new String[]{"new-qualified",packet("","<f:Value rdf:parseType=\"Resource\"><rdf:value>source</rdf:value><f:Unit>unit</f:Unit></f:Value>"),packet("","")});
        cases.add(new String[]{"new-qualified-language",packet("","<f:Value rdf:parseType=\"Resource\"><rdf:value xml:lang=\"en-US\">source</rdf:value><f:Unit>unit</f:Unit></f:Value>"),packet("","")});
        cases.add(new String[]{"new-qualified-uri",packet("","<f:Value rdf:parseType=\"Resource\"><rdf:value rdf:resource=\"urn:source\"/><f:Unit>unit</f:Unit></f:Value>"),packet("","")});
        cases.add(new String[]{"new-qualified-struct",packet("","<f:Value rdf:parseType=\"Resource\"><rdf:value rdf:parseType=\"Resource\"><f:Data>source</f:Data></rdf:value><f:Unit>unit</f:Unit></f:Value>"),packet("","")});
        cases.add(new String[]{"clone-array-empty-item",packet("","<f:Array>"+array("Bag","","source")+"</f:Array>"),packet("","")});
        cases.add(new String[]{"clone-struct-empty-field",packet("","<f:Struct><rdf:Description f:Empty=\"\" f:Data=\"source\"/></f:Struct>"),packet("","")});
        cases.add(new String[]{"dc-alt-repair",packet("","<dc:title>"+array("Seq","source","")+"</dc:title>"),packet("dc:title=\"target\"","")});
        cases.add(new String[]{"namespace-collision",packet("f:Added=\"source\"","<f:Struct><rdf:Description f:Data=\"source\"/></f:Struct>"),packet("f:Kept=\"target\"","").replace("urn:lrtl:paste:test/","urn:lrtl:paste:other/")});
        cases.add(new String[]{"alternate-crs-prefix",packet("crs:Exposure2012=\".75\"","").replace("crs:","develop:").replace("xmlns:crs=","xmlns:develop="),packet("crs:Contrast2012=\"20\"","")});
        cases.add(new String[]{"different-subjects",packet("crs:Exposure2012=\".75\"","").replace("rdf:about=\"\"","rdf:about=\"urn:source\""),packet("xmp:Rating=\"4\"","").replace("rdf:about=\"\"","rdf:about=\"urn:target\"")});
        cases.add(new String[]{"clone-array-duplicates",packet("","<f:Array>"+array("Bag","same","same")+"</f:Array>"),packet("","")});
        cases.add(new String[]{"clone-array-struct-empty",packet("","<f:Array><rdf:Bag><rdf:li><rdf:Description f:Empty=\"\" f:Data=\"source\"/></rdf:li></rdf:Bag></f:Array>"),packet("","")});
        cases.add(new String[]{"clone-nested-empty-only",packet("","<f:Struct><rdf:Description f:Empty=\"\"/></f:Struct>"),packet("","")});
        StringBuilder out=new StringBuilder("[\n");int index=0;
        for(String[] c:cases) {
            System.err.println("Observing "+c[0]);
            Object source=parse.invoke(null,c[1]),target=parse.invoke(null,c[2]);
            clear.invoke(null,target,"http://ns.adobe.com/camera-raw-settings/1.0/");
            append.invoke(null,source,target,true,false);
            String expected=(String)serialize.invoke(null,target,options.getConstructor(int.class).newInstance(16));
            clear.invoke(null,target,"http://ns.adobe.com/camera-raw-settings/1.0/");
            append.invoke(null,source,target,true,false);
            String repeated=(String)serialize.invoke(null,target,options.getConstructor(int.class).newInstance(16));
            if(index++>0)out.append(",\n");
            out.append("{\"Name\":").append(quote(c[0])).append(",\"Source\":").append(quote(c[1])).append(",\"Destination\":").append(quote(c[2])).append(",\"Expected\":").append(quote(expected)).append(",\"Repeated\":").append(quote(repeated)).append('}');
        }
        Files.writeString(Path.of(args[0]),out.append("\n]\n"));
        System.out.println("Observed native XML paste cases: "+index);
    }
}
