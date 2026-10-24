"""Create original metadata-only TIFF/RAW inputs; no camera files are included.

Used only to regenerate reference inputs. Go tests read the checked-in base64
inputs and need neither Python nor ExifTool unless installed replay is enabled.
"""
from pathlib import Path
import base64, json, struct, sys

def encode(directories, endian='<', big=False, cr2=False, magic=42, rw2=False):
    stride=512
    first=24 if rw2 else (16 if big or cr2 else 8)
    offsets=[first+i*stride for i in range(len(directories))]
    data=bytearray(first+len(directories)*stride)
    data[:2]=b'II' if endian=='<' else b'MM'
    if big: struct.pack_into(endian+'HHHQ',data,2,43,8,0,first)
    else:
        struct.pack_into(endian+'HI',data,2,magic,first)
        if cr2: data[8:12]=b'CR\x02\x00';struct.pack_into(endian+'I',data,12,offsets[-1])
        if rw2: data[8:24]=bytes.fromhex('88e774d8f8251d4d947a6e77822b5d6a')
    for i,directory in enumerate(directories):
        fields=[]
        names={'width':256,'height':257,'subfile':254,'oldsubfile':255,'photometric':262,'exifwidth':40962,'exifheight':40963,'crop':50720}
        for name,tag in names.items():
            if name in directory: fields.append((tag,directory.get('kind',4),[directory[name]]))
        for tag,(kind,value) in directory.get('tags',{}).items():
            fields.append((tag,kind,value if isinstance(value,(list,bytes)) else [value]))
        if 'jpeg' in directory: fields.append((46,7,directory['jpeg']))
        for name,tag in [('sub',330),('exif',34665)]:
            if name in directory:
                indices=directory[name] if isinstance(directory[name],list) else [directory[name]]
                fields.append((tag,18 if big else 4,[offsets[j] for j in indices]))
        if directory.get('unsorted'): fields.sort(reverse=True)
        else: fields.sort()
        position=offsets[i]
        struct.pack_into(endian+('Q' if big else 'H'),data,position,len(fields));position+=8 if big else 2
        extra=offsets[i]+384
        for tag,kind,values in fields:
            fmt={1:'B',2:'B',3:'H',4:'I',7:'B',16:'Q',18:'Q'}[kind]
            raw=struct.pack(endian+fmt*len(values),*values)
            width=8 if big else 4
            struct.pack_into(endian+('HHQ' if big else 'HHI'),data,position,tag,kind,len(values))
            if len(raw)>width:
                if kind==7:
                    location=len(data);data.extend(raw)
                else:
                    location=extra;data[extra:extra+len(raw)]=raw;extra+=len(raw)
                raw=struct.pack(endian+('Q' if big else 'I'),location)
            data[position+(12 if big else 8):position+(12 if big else 8)+width]=raw.ljust(width,b'\0')
            position+=20 if big else 12
        struct.pack_into(endian+('Q' if big else 'I'),data,position,offsets[directory['next']] if 'next' in directory else 0)
    return bytes(data)

def cases():
    root={'width':640,'height':480}
    main={'width':6000,'height':4000,'subfile':0}
    definitions=[
        ('root',[root]),('wide',[{'width':70000,'height':80000}]),
        ('max',[{'width':2147483647,'height':1}]),('overflow',[{'width':2147483648,'height':1}]),
        ('missing-width',[{'height':480}]),('missing-height',[{'width':640}]),
        ('zero',[{'width':0,'height':0}]),('short',[dict(root,kind=3)]),
        ('ordinary-sub',[dict(root,sub=1),{'width':6000,'height':4000}]),
        ('main-sub',[dict(root,sub=1),main]),
        ('main-root',[dict(root,sub=1,subfile=0),main]),
        ('reduced-sub',[dict(root,sub=1),dict(main,subfile=1)]),
        ('old-main-sub',[dict(root,sub=1),{'width':6000,'height':4000,'oldsubfile':1}]),
        ('multi-page',[dict(root,next=1),dict(main,subfile=2)]),
        ('next-main',[dict(root,next=1),main]),
        ('two-main',[dict(root,sub=[1,2]),main,{'width':9000,'height':3000,'subfile':0}]),
        ('nested-main',[dict(root,sub=[1,2]),{'width':320,'height':240,'sub':3},{'width':9000,'height':3000,'subfile':0},main]),
        ('exif-ordinary',[dict(root,exif=1),{'exifwidth':6000,'exifheight':4000}]),
        ('exif-only',[{'exif':1},{'exifwidth':6000,'exifheight':4000}]),
        ('cycle',[dict(root,sub=0)]),('next-cycle',[dict(root,next=0)]),
        ('priority-no-width',[dict(root,sub=[1,2]),{'subfile':0,'height':3000},{'width':9000,'height':2000,'subfile':0}]),
        ('unsorted-root',[dict(root,sub=1,subfile=0,unsorted=True),main]),
        ('unsorted-sub',[dict(root,sub=1),dict(main,unsorted=True)]),
        ('missing-root-width-sub',[{'height':480,'sub':1},{'width':6000,'height':4000}]),
    ]
    for endian in ['<','>']:
        for name,dirs in definitions:
            yield {'name':('le-' if endian=='<' else 'be-')+name+'.nef','data':base64.b64encode(encode(dirs,endian)).decode()}
        for suffix in ['dng','arw','sr2','pef','erf','nrw','tif','tiff']:
            yield {'name':('le-' if endian=='<' else 'be-')+'family.'+suffix,'data':base64.b64encode(encode([dict(root,sub=1),main],endian)).decode()}
        for name,dirs in [('cr2-exif',[dict(root,exif=1),{'exifwidth':70000,'exifheight':80000}]),('cr2-no-exif',[root]),('cr2-zero-exif',[dict(root,exif=1),{'exifwidth':0,'exifheight':80000}])]:
            yield {'name':('le-' if endian=='<' else 'be-')+name+'.cr2','data':base64.b64encode(encode(dirs,endian,cr2=True)).decode()}
        for name,dirs in [('big-main',[dict(root,sub=1),main]),('big-wide',[{'width':70000,'height':80000,'kind':16}])]:
            yield {'name':('le-' if endian=='<' else 'be-')+name+'.dng','data':base64.b64encode(encode(dirs,endian,big=True)).decode()}
            yield {'name':('le-' if endian=='<' else 'be-')+name+'.tif','data':base64.b64encode(encode(dirs,endian,big=True)).decode()}

if __name__=='__main__':
    output=Path(sys.argv[1]);output.mkdir(parents=True,exist_ok=False)
    rows=list(cases())
    for row in rows:(output/row['name']).write_bytes(base64.b64decode(row['data']))
    (output/'inputs.json').write_text(json.dumps(rows,indent=1)+'\n',encoding='utf-8')
    print(len(rows))
