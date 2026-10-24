"""Original synthetic ISO BMFF / Canon metadata boxes for dimension replay."""
from pathlib import Path
import base64, json, struct, sys
from raw_dimensions_fixture import encode as tiff
from rw2_dimensions_fixture import JPEG_BASE64

CANON = bytes.fromhex('85c0b687820f11e08111f4ce462b6a48')
PREVIEW = bytes.fromhex('eaf42b5e1c984b88b9fbb7dc406e4d16')

def box(kind, data, mode='normal'):
    kind = kind.encode()
    if mode == 'extended': return struct.pack('>I4sQ',1,kind,len(data)+16)+data
    if mode == 'zero': return struct.pack('>I4s',0,kind)+data
    return struct.pack('>I4s',len(data)+8,kind)+data

def encode(atoms, track=None, mode='normal', uuid=CANON, top=False, padding=b'', track_after=False, version=0):
    children = box('CNCV',b'CanonCR3_001\0') + b''.join(box(name,data) for name,data in atoms)
    metadata = box('uuid',uuid+children,mode)
    if track is not None:
        header=bytearray(96 if version else 84);header[0]=version
        struct.pack_into('>II',header,88 if version else 76,int(track[0]*65536),int(track[1]*65536))
        trackbox=box('trak',box('tkhd',header))
        metadata=metadata+trackbox if track_after else trackbox+metadata
    return box('ftyp',b'crx \0\0\0\0crx isom')+(metadata if top else box('moov',metadata))+padding

def cases():
    for endian in ('<','>'):
        prefix='le-' if endian=='<' else 'be-'
        definitions=[
            ('root',[{'width':6000,'height':4000}]),
            ('main',[{'width':6000,'height':4000,'subfile':0}]),
            ('reduced-sub',[{'width':64,'height':32,'sub':1},{'width':6000,'height':4000,'subfile':0}]),
            ('ordinary-sub',[{'width':64,'height':32,'sub':1},{'width':6000,'height':4000}]),
            ('exif',[{'width':6000,'height':4000,'exif':1},{'exifwidth':8000,'exifheight':5000}]),
            ('exif-only',[{'exifwidth':8000,'exifheight':5000}]),
            ('next-main',[{'width':64,'height':32,'next':1},{'width':6000,'height':4000,'subfile':0}]),
            ('zero',[{'width':0,'height':0}]),
            ('zero-width',[{'width':0,'height':4000}]), ('zero-height',[{'width':6000,'height':0}]),
            ('missing-width',[{'height':4000}]), ('missing-height',[{'width':6000}]),
            ('wide',[{'width':70000,'height':80000}]),
            ('max',[{'width':2147483647,'height':2147483647}]),
            ('overflow',[{'width':2147483648,'height':4000}]),
            ('short',[{'width':6000,'height':4000,'kind':3}]),
            ('big',[{'width':6000,'height':4000}]),
        ]
        for name,dirs in definitions:
            yield prefix+name,encode([('CMT1',tiff(dirs,endian,big=name=='big'))])
        first=tiff([{'width':6000,'height':4000}],endian)
        second=tiff([{'width':320,'height':240}],endian)
        exif=tiff([{'exifwidth':8000,'exifheight':5000}],endian)
        main=tiff([{'width':320,'height':240,'subfile':0}],endian)
        for name,atoms in [
            ('cmt2-only',[('CMT2',first)]), ('cmt2-exif-only',[('CMT2',exif)]),
            ('cmt1-cmt2',[('CMT1',first),('CMT2',second)]),
            ('cmt2-cmt1',[('CMT2',second),('CMT1',first)]),
            ('cmt1-exif',[('CMT1',first),('CMT2',exif)]),
            ('cmt1-duplicate',[('CMT1',first),('CMT1',second)]),
            ('cmt1-next-main',[('CMT1',first),('CMT1',main)]),
            ('split-axes',[('CMT1',tiff([{'width':6000}],endian)),('CMT2',tiff([{'height':4000}],endian))]),
            ('zero-first',[('CMT1',tiff([{'width':0,'height':0}],endian)),('CMT2',first)]),
        ]:yield prefix+name,encode(atoms)
        for name,opts in [('track',{'track':(1920,1080)}),('extended',{'mode':'extended'}),('zero-box',{'mode':'zero'}),('top',{'top':True}),('unknown-uuid',{'uuid':bytes(16)})]:
            yield prefix+name,encode([('CMT1',first)],**opts)
        for name,opts in [('track-after',{'track_after':True}),('track-v1',{'version':1})]:
            yield prefix+name,encode([('CMT1',first)],track=(1920,1080),**opts)
        yield prefix+'track-fraction',encode([('CMT1',first)],track=(1920.5,1080))
        yield prefix+'track-zero',encode([('CMT1',first)],track=(0,0))
        yield prefix+'track-zero-width',encode([('CMT1',first)],track=(0,1080))
    yield 'track-only',encode([],track=(1920,1080))
    yield 'empty',encode([])
    preview=bytearray(64)
    preview[:16]=PREVIEW
    struct.pack_into('>HH',preview,0x36,64,32)
    jpeg=base64.b64decode(JPEG_BASE64)
    struct.pack_into('>I',preview,0x3c,len(jpeg))
    yield 'preview-only',encode([],padding=box('uuid',bytes(preview)+jpeg))
    yield 'preview-cmt1',encode([('CMT1',tiff([{'width':6000,'height':4000}]))],padding=box('uuid',bytes(preview)+jpeg))

if __name__=='__main__':
    out=Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=False)
    rows=[];seen=set()
    for name,data in cases():
        name+='.cr3';assert name not in seen;seen.add(name);(out/name).write_bytes(data)
        rows.append({'name':name,'data':base64.b64encode(data).decode()})
    (out/'inputs.json').write_text(json.dumps(rows,indent=1)+'\n',encoding='utf-8')
    print(len(rows))
