"""Original image-only XMP capture-date inputs for the native file reader."""
from pathlib import Path
import os
import sys
from raw_dimensions_fixture import encode
from xmp_multiple_fixture import packet, jpeg

def date(prefix,name,value):
    uri='http://ns.adobe.com/exif/1.0/' if prefix=='exif' else 'http://ns.adobe.com/xap/1.0/'
    return f'<{prefix}:{name} xmlns:{prefix}="{uri}">{value}</{prefix}:{name}>'

def cases():
    original=date('exif','DateTimeOriginal','2026-10-24T03:04:05.123+09:00')
    create=date('xmp','CreateDate','2027-01-02T03:04:05Z')
    modify=date('xmp','ModifyDate','2028-01-02T03:04:05Z')
    digitized=date('exif','DateTimeDigitized','2025-01-02T03:04:05Z')
    nested='<f:Preset><rdf:Description xmlns:e="http://ns.adobe.com/exif/1.0/" e:DateTimeOriginal="2000-01-01T00:00:00Z"/></f:Preset>'
    definitions={
        'original':original,
        'create':create,
        'modify':modify,
        'all':original+create+modify+digitized+nested,
        'nested-only':nested,
        'nested-create':nested+create,
        'colon-original':date('exif','DateTimeOriginal','2026:10:24 03:04:05'),
        'space-original':date('exif','DateTimeOriginal','2026-10-24 03:04:05'),
        'fraction-no-zone':date('exif','DateTimeOriginal','2026-10-24T03:04:05.123'),
        'invalid-original':date('exif','DateTimeOriginal','invalid')+create,
        'digitized-only':digitized,
        'alias-attribute':'',
        'alias-foreign-prefix':'',
        'alias-qualifier':'<e:DateTimeDigitized xmlns:e="http://ns.adobe.com/exif/1.0/" xml:lang="en-US">2025-01-02T03:04:05Z</e:DateTimeDigitized>',
        'alias-nested':create+'<f:Preset><rdf:Description xmlns:e="http://ns.adobe.com/exif/1.0/" e:DateTimeDigitized="2000-01-01T00:00:00Z"/></f:Preset>',
    }
    for name,body in definitions.items():
        xml=packet('<crs:Exposure2012>0.25</crs:Exposure2012><f:Keep>unchanged</f:Keep>'+body)
        if name in ['alias-attribute','alias-foreign-prefix']:
            attributes=b'xmlns:e="http://ns.adobe.com/exif/1.0/" e:DateTimeDigitized="2025-01-02T03:04:05Z" '
            if name=='alias-foreign-prefix':attributes+=b'xmlns:xmp="urn:lrtl:foreign" xmp:Keep="untouched" '
            xml=xml.replace(b'<rdf:Description ',b'<rdf:Description '+attributes,1)
        for extension in ['jpg','tif','dng']:
            data=jpeg([xml]) if extension=='jpg' else encode([{'width':64,'height':32,'tags':{700:(7,xml)}}])
            yield extension+'-'+name,extension,data

if __name__=='__main__':
    root=Path(sys.argv[1]);root.mkdir(parents=True,exist_ok=False)
    count=0
    for name,extension,data in cases():
        directory=root/name;directory.mkdir()
        path=directory/('frame.'+extension);path.write_bytes(data)
        os.utime(path,ns=(1790000000000000000,1790000000000000000));count+=1
    print(count)
