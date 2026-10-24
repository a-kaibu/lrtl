"""Original synthetic RAF metadata/preview cases for installed ImageSize replay.

The JPEG is the original solid-color fixture used by the RW2 observations.
No camera image or product resource is included.
"""
import base64, json, struct, sys
from pathlib import Path
from rw2_dimensions_fixture import JPEG_BASE64
from raw_dimensions_fixture import encode as tiff


def directory(entries):
    return struct.pack('>I', len(entries)) + b''.join(struct.pack('>HH', tag, len(value)) + value for tag, value in entries)


def pair(tag, width, height):
    return tag, struct.pack('>HH', height, width)


def encode(jpeg, directories=(), ifds=(), jpos=160, signature=b'FUJIFILMCCD-RAW ', lengths=None):
    data = bytearray(max(jpos, 160))
    data[:16] = signature
    data[16:20] = b'0201'
    data[28:60] = b'lrtl synthetic RAF'.ljust(32, b'\0')
    data[60:64] = b'0100'
    def append(pointer, value, length=None):
        offset = len(data)
        data.extend(value)
        struct.pack_into('>II', data, pointer, offset, len(value) if length is None else length)
    if jpeg is not None:
        append(84, jpeg)
    for i, entries in enumerate(directories):
        append([92,120][i], directory(entries), None if lengths is None else lengths[i])
    for i, value in enumerate(ifds):
        append([100,128][i], value)
    return bytes(data)


def cases():
    jpeg = base64.b64decode(JPEG_BASE64)
    crop = pair(0x111, 6000, 4000)
    full = pair(0x100, 6016, 4016)
    for name, entries in [
        ('crop',[crop]), ('full-only',[full]), ('empty',[]),
        ('full-crop',[full,crop]), ('crop-full',[crop,full]),
        ('zero-width',[pair(0x111,0,4000)]), ('zero-height',[pair(0x111,6000,0)]),
        ('zero',[pair(0x111,0,0)]), ('max',[pair(0x111,65535,65535)]),
        ('portrait',[pair(0x111,4000,6000)]), ('one',[pair(0x111,1,1)]),
        ('short-crop',[(0x111,b'\x00\x02')]), ('empty-crop',[(0x111,b'')]),
        ('byte-crop',[(0x111,b'\x02')]), ('odd-crop',[(0x111,b'\x00\x02\x03')]),
        ('odd-extra-crop',[(0x111,struct.pack('>HH',4000,6000)+b'x')]),
        ('extra-crop',[(0x111,struct.pack('>HHH',4000,6000,9000))]),
        ('duplicate',[crop,pair(0x111,320,240)]),
        ('duplicate-empty',[(0x111,b''),crop]),
        ('duplicate-zero',[pair(0x111,0,0),crop]),
        ('unknown',[(0xbeef,b'ignored'),crop]),
        ('unknown-long',[(0xbeef,b'a'*4096),crop]),
        ('boundary-255',[(0x8000+i,b'x') for i in range(254)]+[crop]),
        ('boundary-256',[(0x8000+i,b'x') for i in range(255)]+[crop]),
    ]:
        yield name, encode(jpeg,[entries])
    for name, dirs in [
        ('two-first',[[crop],[pair(0x111,320,240)]]),
        ('two-second',[[full],[crop]]), ('two-empty',[[],[crop]]),
        ('two-zero',[[pair(0x111,0,0)],[crop]]),
        ('two-invalid-count',[[(0x8000+i,b'x') for i in range(256)],[crop]]),
    ]:
        yield name, encode(jpeg,dirs)
    for name, preview in [('no-jpeg',None),('empty-jpeg',b''),('bad-jpeg',b'bad JPEG')]:
        yield name+'-crop', encode(preview,[[crop]])
        yield name+'-full', encode(preview,[[full]])
    yield 'jpeg-only', encode(jpeg)
    yield 'crop-length-zero', encode(jpeg,[[crop]], lengths=[0])
    yield 'crop-length-short', encode(jpeg,[[crop]], lengths=[4])
    yield 'crop-length-long', encode(jpeg,[[crop]], lengths=[0xffffffff])
    yield 'jpeg-high-bit', encode(jpeg,[[crop]], jpos=0x8000)
    yield 'jpeg-large-offset', encode(jpeg,[[crop]], jpos=0x10000)
    yield 'jpeg-high-offset-bit', encode(jpeg,[[crop]], jpos=0x18000)
    for endian in ['<','>']:
        prefix = 'le-' if endian=='<' else 'be-'
        for name, fields in [('ifd-ordinary',{'width':320,'height':240}),('ifd-main',{'width':320,'height':240,'subfile':0}),('ifd-exif-only',{'exifwidth':5000,'exifheight':3000})]:
            block = tiff([fields], endian)
            yield prefix+name, encode(jpeg,ifds=[block])
            yield prefix+name+'-crop', encode(jpeg,[[crop]],ifds=[block])
            yield prefix+name+'-no-jpeg', encode(None,[[crop]],ifds=[block])
        exif = tiff([{'width':700,'height':500,'exif':1},{'exifwidth':800,'exifheight':600}],endian)
        app = b'Exif\0\0'+exif
        preview = jpeg[:2]+b'\xff\xe1'+struct.pack('>H',len(app)+2)+app+jpeg[2:]
        yield prefix+'jpeg-exif', encode(preview)
        yield prefix+'jpeg-exif-crop', encode(preview,[[crop]])
    for name,pos in [('jpeg-at-112',112),('jpeg-at-128',128)]:
        # encode reserves 160 bytes; move the first JPEG and update offsets.
        data = bytearray(encode(jpeg,[[crop]]))
        del data[pos:160]
        for pointer in (84,92):
            old = struct.unpack_from('>I',data,pointer)[0]
            struct.pack_into('>I',data,pointer,old-(160-pos))
        yield name, bytes(data)


if __name__ == '__main__':
    output = Path(sys.argv[1]); output.mkdir(parents=True,exist_ok=False)
    rows=[]
    for name,data in cases():
        name += '.raf'
        (output/name).write_bytes(data)
        rows.append({'name':name,'data':base64.b64encode(data).decode()})
    (output/'inputs.json').write_text(json.dumps(rows,indent=1)+'\n',encoding='utf-8')
    print(len(rows))
