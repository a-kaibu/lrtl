"""Original synthetic files and fixed timestamps for native XMP source loading."""
import base64, json, struct, sys, zlib, os
from pathlib import Path
from raw_dimensions_fixture import encode
from rw2_dimensions_fixture import JPEG_BASE64

PNG_BASE64 = "iVBORw0KGgoAAAANSUhEUgAAAEAAAAAgCAIAAAAt/+nTAAAAUUlEQVR4nNXOQREAIAzAsFIhKEEO/jUgYg+uUZC1z6VM4iRO4iRO4iRO4iRO4iRO4iRO4iRO4iRO4iRO4iRO4iRO4iRO4iRO4iRO4iTO34GpBwNZAP46OczNAAAAAElFTkSuQmCC"

def packet(origin):
    exposure='1' if origin=='embedded' else '2'
    return f'''<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:f="urn:lrtl:xmp-source" crs:Exposure2012="{exposure}" f:Origin="{origin}"/></rdf:RDF></x:xmpmeta>'''.encode()

def image(extension,embedded):
    payload=packet('embedded')
    if extension in ['jpg','jpeg']:
        data=base64.b64decode(JPEG_BASE64)
        app=b'http://ns.adobe.com/xap/1.0/\0'+payload
        return data[:2]+b'\xff\xe1'+struct.pack('>H',len(app)+2)+app+data[2:] if embedded else data
    if extension=='png':
        data=base64.b64decode(PNG_BASE64)
        if not embedded:return data
        app=b'XML:com.adobe.xmp\0\0\0\0\0'+payload
        chunk=b'iTXt'+app
        return data[:33]+struct.pack('>I',len(app))+chunk+struct.pack('>I',zlib.crc32(chunk))+data[33:]
    fields={700:(1,payload)} if embedded else {}
    return encode([{'width':64,'height':32,'tags':fields}])

if __name__=='__main__':
    output=Path(sys.argv[1]);output.mkdir(parents=True,exist_ok=False)
    stamp=1790000000000
    for extension in ['jpg','jpeg','dng','tif','tiff','png']:
        for embedded in [False,True]:
            for side in ['none','older','equal','newer','bad-older','bad-newer']:
                directory=output/f'{extension}-embedded{int(embedded)}-{side}';directory.mkdir()
                path=directory/('frame.'+extension);path.write_bytes(image(extension,embedded));os.utime(path,ns=(stamp*1000000,stamp*1000000))
                if side!='none':
                    data=b'<invalid' if side.startswith('bad') else packet('sidecar')
                    sidecar=directory/'frame.xmp';sidecar.write_bytes(data)
                    time=stamp+(2000 if side.endswith('newer') else (-2000 if side.endswith('older') else 0));os.utime(sidecar,ns=(time*1000000,time*1000000))
    for extension in ['jpg','dng']:
        for name,image_offset,sidecar_offset in [('subms-newer',100000,900000),('next-ms',900000,1100000)]:
            directory=output/(extension+'-'+name);directory.mkdir()
            path=directory/('frame.'+extension);path.write_bytes(image(extension,True))
            sidecar=directory/'frame.xmp';sidecar.write_bytes(packet('sidecar'))
            image_ns=stamp*1000000+image_offset;sidecar_ns=stamp*1000000+sidecar_offset
            os.utime(path,ns=(image_ns,image_ns));os.utime(sidecar,ns=(sidecar_ns,sidecar_ns))
    print(76)
