"""Original format/header matrix for the complete native file-level XMP reader."""
from pathlib import Path
import base64, os, struct, sys
from raw_dimensions_fixture import encode
from rw2_dimensions_fixture import JPEG_BASE64
from xmp_source_fixture import image, packet
from raf_dimensions_fixture import encode as raf
from cr3_dimensions_fixture import box, encode as cr3, PREVIEW

TIFF_TYPES = ['nef','nrw','arw','sr2','pef','erf','orf','dcr','rw2','rwl','raw','iiq','3fr','fff','dcs','kdc','mef','mos','srw','srf','cr2','dng','tif','tiff']

def tiff_image(extension, endian):
    jpeg=base64.b64decode(JPEG_BASE64)
    dirs=[{'width':64,'height':32,'tags':{700:(7,packet('embedded'))},'next':1},{'tags':{513:(4,0),514:(4,len(jpeg))}}]
    panasonic=extension in ['rw2','rwl','raw']
    data=bytearray(encode(dirs,endian,cr2=extension=='cr2',magic=85 if panasonic else 42,rw2=panasonic))
    first=struct.unpack_from(endian+'I',data,4)[0]
    if extension=='orf':struct.pack_into(endian+'H',data,2,0x4f52)
    offset=first+512;count=struct.unpack_from(endian+'H',data,offset)[0]
    for i in range(count):
        entry=offset+2+i*12
        if struct.unpack_from(endian+'H',data,entry)[0]==513:struct.pack_into(endian+'I',data,entry+8,len(data))
    data.extend(jpeg)
    return bytes(data)

def cases():
    for endian in ['<','>']:
        for extension in TIFF_TYPES:
            yield ('le-' if endian=='<' else 'be-')+extension,extension,tiff_image(extension,endian)
    for extension in ['jpg','jpeg','png']:
        yield extension,extension,image(extension,True)
    jpeg=image('jpg',True)
    yield 'raf','raf',raf(jpeg)
    preview=bytearray(64);preview[:16]=PREVIEW;struct.pack_into('>I',preview,0x3c,len(jpeg))
    xmp=box('uuid',bytes.fromhex('be7acfcb97a942e89c71999491e3afac')+packet('embedded'))
    yield 'cr3','cr3',cr3([('CMT1',encode([{'width':64,'height':32}]))],padding=xmp+box('uuid',bytes(preview)+jpeg))
    # The native dispatch uses the suffix, so include uppercase variants and a
    # JPEG-looking payload under a RAW suffix without assuming content dispatch.
    for extension in ['NEF','DNG','TIF','JPG','CR3','RAF']:
        yield 'uppercase-'+extension,extension,next(data for _,ext,data in list(cases_lower()) if ext==extension.lower())
    yield 'jpeg-as-nef','nef',jpeg

def cases_lower():
    for extension in TIFF_TYPES:yield extension,extension,tiff_image(extension,'<')
    for extension in ['jpg','jpeg','png']:yield extension,extension,image(extension,True)
    jpeg=image('jpg',True)
    yield 'raf','raf',raf(jpeg)
    yield 'cr3','cr3',cr3([],padding=box('uuid',bytes.fromhex('be7acfcb97a942e89c71999491e3afac')+packet('embedded')))

if __name__=='__main__':
    out=Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=False);count=0
    stamp=1900000000000000000
    for name,extension,data in cases():
        for side in ['none','older','equal','newer','bad-older','bad-equal','bad-newer']:
            directory=out/(name+'-'+side);directory.mkdir()
            path=directory/('frame.'+extension);path.write_bytes(data);os.utime(path,ns=(stamp,stamp))
            if side!='none':
                path=directory/'frame.xmp';path.write_bytes(b'<invalid' if side.startswith('bad') else packet('sidecar'))
                time=stamp+(2000000000 if side.endswith('newer') else (-2000000000 if side.endswith('older') else 0));os.utime(path,ns=(time,time))
            count+=1
    print(count)
