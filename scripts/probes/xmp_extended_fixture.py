"""Original standard/extended APP1 XMP fixtures for the native file reader."""
import base64
import hashlib
import os
from pathlib import Path
import struct
import sys
from rw2_dimensions_fixture import JPEG_BASE64
from xmp_multiple_fixture import packet

STANDARD = b'http://ns.adobe.com/xap/1.0/\0'
EXTENDED = b'http://ns.adobe.com/xmp/extension/\0'

def segment(header, payload):
    data = header + payload
    return b'\xff\xe1' + struct.pack('>H', len(data) + 2) + data

def standard(body, guid=None):
    link = '' if guid is None else '<n:HasExtendedXMP xmlns:n="http://ns.adobe.com/xmp/note/">'+guid+'</n:HasExtendedXMP>'
    return segment(STANDARD, packet(body + link))

def extended(payload, guid=None, cuts=None):
    guid = guid or hashlib.md5(payload).hexdigest().upper()
    cuts = cuts or [0, len(payload)]
    chunks = [segment(EXTENDED, guid.encode()+struct.pack('>II',len(payload),a)+payload[a:b]) for a,b in zip(cuts,cuts[1:])]
    return guid,chunks

def cases():
    first='<crs:Exposure2012>1</crs:Exposure2012><f:First>A</f:First>'
    second='<crs:Exposure2012>2</crs:Exposure2012><f:Second>B</f:Second>'
    payload=packet(second)
    guid,chunks=extended(payload)
    std=standard(first,guid)
    result={
        'linked':[std]+chunks,
        'chunks-before-standard':chunks+[std],
        'no-link':[standard(first)]+chunks,
        'no-standard':chunks,
        'missing-extension':[std],
        'lowercase-link':[standard(first,guid.lower())]+chunks,
        'lowercase-chunk':[std]+extended(payload,guid.lower())[1],
        'lowercase-both':[standard(first,guid.lower())]+extended(payload,guid.lower())[1],
        'wrong-digest':[standard(first,'0'*32)]+extended(payload,'0'*32)[1],
        'invalid-link':[standard(first,'invalid')]+chunks,
        'empty-link':[standard(first,'')]+chunks,
        'second-standard-no-link':[std,standard('<f:Third>C</f:Third>')]+chunks,
        'second-standard-conflict':[std,standard('<crs:Exposure2012>3</crs:Exposure2012>')]+chunks,
    }
    for name,body in {
        'empty-extension':'',
        'empty-values':'<crs:Exposure2012/><f:First/>',
        'disjoint':'<f:Second>B</f:Second>',
        'struct':'<f:First rdf:parseType="Resource"><f:B>2</f:B></f:First>',
        'array':'<f:First><rdf:Seq><rdf:li>B</rdf:li></rdf:Seq></f:First>',
    }.items():
        g,cs=extended(packet(body))
        result[name]=[standard(first,g)]+cs
    g,cs=extended(payload,cuts=[0,37,151,len(payload)])
    result['three-chunks']=[standard(first,g)]+cs
    result['reverse-chunks']=[standard(first,g)]+list(reversed(cs))
    result['duplicate-chunk']=[standard(first,g)]+cs+[cs[0]]
    result['gap']=[standard(first,g),cs[0],cs[2]]
    result['duplicate-packet']=[std]+chunks+chunks
    g,cs=extended(b'<invalid')
    result['bad-extension-xml']=[standard(first,g)]+cs
    result['unrelated-extension']=[standard(first,'A'*32)]+chunks
    result['standard-after-extension']=[std]+chunks+[standard('<crs:Exposure2012>3</crs:Exposure2012>')]
    g2,cs2=extended(packet('<crs:Exposure2012>4</crs:Exposure2012><f:Fourth>D</f:Fourth>'))
    result['two-links']=[std]+chunks+[standard('<f:Third>C</f:Third>',g2)]+cs2
    result['two-links-before-chunks']=[std,standard('<f:Third>C</f:Third>',g2)]+chunks+cs2
    result['second-link-missing']=[std]+chunks+[standard('<f:Third>C</f:Third>',g2)]
    result['same-link-twice']=[std]+chunks+[standard('<f:Third>C</f:Third>',guid)]+chunks
    return result

if __name__=='__main__':
    root=Path(sys.argv[1]);root.mkdir(parents=True,exist_ok=False)
    image=base64.b64decode(JPEG_BASE64)
    for name,segments in cases().items():
        directory=root/name;directory.mkdir()
        path=directory/'frame.jpg'
        path.write_bytes(image[:2]+b''.join(segments)+image[2:])
        os.utime(path,ns=(1790000000000000000,1790000000000000000))
    print(len(cases()))
