"""Original TIFF-based Phase One dimension and JPEG/XMP fixtures."""
from pathlib import Path
import base64, json, struct, sys
from raw_dimensions_fixture import encode
from rw2_dimensions_fixture import JPEG_BASE64

def with_preview(endian):
    jpeg=base64.b64decode(JPEG_BASE64)
    packet=b'<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:f="urn:lrtl:iiq:fixture" crs:Exposure2012="0.5" f:Keep="embedded"/></rdf:RDF>'
    dirs=[{'width':64,'height':32,'next':1,'exif':2,'tags':{700:(7,packet)}},{'tags':{513:(4,0),514:(4,len(jpeg))}},{'exifwidth':6000,'exifheight':4000}]
    data=bytearray(encode(dirs,endian));offset=struct.unpack_from(endian+'I',data,4)[0]+512
    count=struct.unpack_from(endian+'H',data,offset)[0]
    for i in range(count):
        entry=offset+2+12*i
        if struct.unpack_from(endian+'H',data,entry)[0]==513:struct.pack_into(endian+'I',data,entry+8,len(data))
    data.extend(jpeg)
    return bytes(data)

def cases():
    definitions=[
        ('root',[{'width':640,'height':480}]),
        ('exif',[{'width':640,'height':480,'exif':1},{'exifwidth':6000,'exifheight':4000}]),
        ('exif-only',[{'exifwidth':6000,'exifheight':4000}]),
        ('zero-exif',[{'width':640,'height':480,'exif':1},{'exifwidth':0,'exifheight':0}]),
        ('zero-exif-width',[{'width':640,'height':480,'exif':1},{'exifwidth':0,'exifheight':4000}]),
        ('missing-exif-width',[{'width':640,'height':480,'exif':1},{'exifheight':4000}]),
        ('exif-main',[{'width':640,'height':480,'sub':1,'exif':2},{'width':8000,'height':5000,'subfile':0},{'exifwidth':6000,'exifheight':4000}]),
        ('exif-last',[{'width':640,'height':480,'exif':1,'next':2},{'exifwidth':6000,'exifheight':4000},{'exifwidth':7000,'exifheight':4500}]),
        ('wide-exif',[{'width':640,'height':480,'exif':1},{'exifwidth':70000,'exifheight':80000}]),
        ('overflow-exif',[{'width':640,'height':480,'exif':1},{'exifwidth':2147483648,'exifheight':4000}]),
        ('overflow-unused',[{'width':640,'height':480,'exif':1},{'exifwidth':2147483648}]),
        ('make-phaseone',[{'width':640,'height':480,'exif':1,'tags':{271:(2,b'Phase One\0')}},{'exifwidth':6000,'exifheight':4000}]),
    ]
    for endian in ('<','>'):
        prefix='le-' if endian=='<' else 'be-'
        for name,dirs in definitions:
            for suffix in ('iiq','tif'):
                yield prefix+name+'.'+suffix,encode(dirs,endian)
        yield prefix+'big.iiq',encode([{'width':640,'height':480}],endian,big=True)
        yield prefix+'preview.iiq',with_preview(endian)

if __name__=='__main__':
    out=Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=False)
    rows=[];seen=set()
    for name,data in cases():
        assert name not in seen;seen.add(name);(out/name).write_bytes(data)
        rows.append({'name':name,'data':base64.b64encode(data).decode()})
    (out/'inputs.json').write_text(json.dumps(rows,indent=1)+'\n',encoding='utf-8');print(len(rows))
