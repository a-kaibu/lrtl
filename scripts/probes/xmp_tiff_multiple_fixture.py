"""Original TIFF/DNG directory graphs with synthetic tag-700 packets."""
from pathlib import Path
import os
import sys
from raw_dimensions_fixture import encode
from xmp_multiple_fixture import packet

def node(label, **edges):
    exposure={'A':1,'B':2,'C':3,'D':4}[label]
    return dict(width=64,height=32,tags={700:(7,packet(f'<crs:Exposure2012>{exposure}</crs:Exposure2012><f:{label}>{label}</f:{label}>'))},**edges)

def cases():
    definitions={
        'root':[node('A')],
        'next':[node('A',next=1),node('B')],
        'sub':[node('A',sub=1),node('B')],
        'exif':[node('A',exif=1),node('B')],
        'sub-and-next':[node('A',sub=1,next=2),node('B'),node('C')],
        'sub-siblings-nested':[node('A',sub=[1,2]),node('B',sub=3),node('C'),node('D')],
        'sub-next':[node('A',sub=1),node('B',next=2),node('C')],
        'next-sub':[node('A',next=1),node('B',sub=2),node('C')],
        'exif-sub':[node('A',exif=1,sub=2),node('B'),node('C')],
        'shared-child':[node('A',sub=[1,1],next=1),node('B')],
        'cycle-sub':[node('A',sub=1),node('B',sub=0)],
        'cycle-next':[node('A',next=1),node('B',next=0)],
        'unsorted-sub':[dict(node('A',sub=1),unsorted=True),node('B')],
        'empty-root':[{'width':64,'height':32,'sub':1},node('B')],
        'bad-first':[dict(node('A',next=1),tags={700:(7,b'<invalid')}),node('B')],
        'bad-last':[node('A',next=1),dict(node('B'),tags={700:(7,b'<invalid')})],
        'gps':[dict(node('A'),tags={**node('A')['tags'],34853:(4,[520])}),node('B')],
        'interop':[dict(node('A'),tags={**node('A')['tags'],40965:(4,[520])}),node('B')],
        'exif-next':[node('A',exif=1),node('B',next=2),node('C')],
        'empty-root-next':[{'width':64,'height':32,'next':1},node('B')],
    }
    for extension in ['tif','tiff','dng']:
        for endian in ['<','>']:
            for name,directories in definitions.items():
                yield f'{extension}-'+('le-' if endian=='<' else 'be-')+name,extension,encode(directories,endian)

if __name__=='__main__':
    root=Path(sys.argv[1]);root.mkdir(parents=True,exist_ok=False)
    count=0
    for name,extension,data in cases():
        directory=root/name;directory.mkdir()
        path=directory/('frame.'+extension);path.write_bytes(data)
        os.utime(path,ns=(1790000000000000000,1790000000000000000));count+=1
    print(count)
