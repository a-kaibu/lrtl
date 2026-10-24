"""Original EXIF date inputs plus actual installed ExifTool date columns."""
import base64,csv,json,struct,subprocess,sys,zlib
from pathlib import Path
from raw_dimensions_fixture import encode
from rw2_dimensions_fixture import JPEG_BASE64
from xmp_source_fixture import PNG_BASE64

def cases():
    definitions=[('original',None,None,True,True),('fallback',None,None,False,True),('missing',None,None,False,False)]
    for sub in ['1','12','123','123456','100','01','001','000','0','1000']:
        for offset in [None,'+09:00','-03:30']:
            definitions.append(('sub-'+sub+'-'+(offset.replace(':','') if offset else 'none'),sub,offset,True,True))
    for extension in ['jpg','png','tif','dng']:
        for endian in ['<','>']:
            for name,sub,offset,original,fallback in definitions:
                tags={}
                def add(tag,text):tags[tag]=(2,text.encode()+b'\0')
                if original:add(36867,'2026:10:24 03:04:05')
                if fallback:add(36868,'2020:01:02 03:04:05')
                if sub is not None:add(37521,sub)
                if offset is not None:add(36881,offset)
                data=encode([{'width':64,'height':32,'exif':1},{'tags':tags}],endian)
                if extension=='jpg':
                    image=base64.b64decode(JPEG_BASE64);payload=b'Exif\0\0'+data
                    data=image[:2]+b'\xff\xe1'+struct.pack('>H',len(payload)+2)+payload+image[2:]
                if extension=='png':
                    image=base64.b64decode(PNG_BASE64);chunk=b'eXIf'+data
                    data=image[:33]+struct.pack('>I',len(data))+chunk+struct.pack('>I',zlib.crc32(chunk))+image[33:]
                yield ('le-' if endian=='<' else 'be-')+name+'.'+extension,data
    reference=Path(__file__).resolve().parents[2]/'internal/lrtl/testdata/lrt-embedded-capture-time-7.5.5.json'
    for row in json.loads(reference.read_text(encoding='utf-8'))['cases']:
        yield 'xmp-'+row['name']+Path(row['filename']).suffix,base64.b64decode(row['data'])

if __name__=='__main__':
    root=Path(sys.argv[1]);root.mkdir(parents=True,exist_ok=False)
    paths=[]
    for name,data in cases():
        path=root/name;path.write_bytes(data);paths.append(str(path.resolve()))
    output=subprocess.check_output([sys.argv[2],'-csv','-f','-DateTimeOriginal','-CreateDate','-SubSecDateTimeOriginal',*paths])
    rows=list(csv.DictReader(output.decode('utf-8-sig').splitlines()))
    with (root/'metadata.csv').open('w',encoding='utf-8',newline='') as f:
        writer=csv.writer(f)
        for row in rows:
            fields=['-']*16;fields[0]=row['SourceFile'];fields[1]='64x32'
            fields[2]=row['DateTimeOriginal'];fields[9]=row['CreateDate'];fields[12]=row['SubSecDateTimeOriginal'];writer.writerow(fields)
    print(len(rows))
