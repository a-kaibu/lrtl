"""Original multiple-standard-APP1 XMP fixtures; no camera or product data."""
import base64, struct, sys
from pathlib import Path
from rw2_dimensions_fixture import JPEG_BASE64

def packet(body):
    return ('<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/" xmlns:f="urn:lrtl:multiple">'+body+'</rdf:Description></rdf:RDF></x:xmpmeta>').encode()

def jpeg(packets):
    data=base64.b64decode(JPEG_BASE64)
    segments=b''
    for p in packets:
        payload=b'http://ns.adobe.com/xap/1.0/\0'+p
        segments+=b'\xff\xe1'+struct.pack('>H',len(payload)+2)+payload
    return data[:2]+segments+data[2:]

def cases():
    a=packet('<crs:Exposure2012>1</crs:Exposure2012><f:First>A</f:First>')
    b=packet('<crs:Exposure2012>2</crs:Exposure2012><f:Second>B</f:Second>')
    empty=packet('')
    result={'single':[a], 'disjoint-conflict':[a,b], 'reverse':[b,a], 'three':[a,b,packet('<crs:Exposure2012>3</crs:Exposure2012><f:Third>C</f:Third>')], 'empty-last':[a,empty], 'empty-first':[empty,a], 'bad-last':[a,b'<invalid'], 'bad-first':[b'<invalid',a], 'duplicate':[a,a], 'empty-conflict':[a,packet('<crs:Exposure2012/><f:First/>')], 'struct':[packet('<f:Value rdf:parseType="Resource"><f:A>1</f:A></f:Value>'),packet('<f:Value rdf:parseType="Resource"><f:B>2</f:B></f:Value>')], 'array':[packet('<f:Value><rdf:Seq><rdf:li>A</rdf:li><rdf:li>B</rdf:li></rdf:Seq></f:Value>'),packet('<f:Value><rdf:Seq><rdf:li>B</rdf:li><rdf:li>C</rdf:li></rdf:Seq></f:Value>')]}

    for name,body in {
        'array-empty':'<f:Value><rdf:Seq/></f:Value>',
        'struct-empty':'<f:Value rdf:parseType="Resource"/>',
        'struct-empty-field':'<f:Value rdf:parseType="Resource"><f:B/></f:Value>',
        'array-empty-item':'<f:Value><rdf:Seq><rdf:li/></rdf:Seq></f:Value>',
        'type-change':'<f:Value>scalar</f:Value>',
        'qualified':'<f:Value rdf:parseType="Resource"><rdf:value>second</rdf:value><f:Q>qualifier</f:Q></f:Value>',
    }.items():
        result[name]=[packet('<f:Value rdf:parseType="Resource"><f:A>1</f:A></f:Value>'),packet(body)]
    result['all-bad']=[b'<invalid',b'<invalid']
    result['single-bad']=[b'<invalid']
    return result

if __name__=='__main__':
    output=Path(sys.argv[1]);output.mkdir(parents=True,exist_ok=False)
    for name,packets in cases().items():
        directory=output/name;directory.mkdir()
        (directory/'frame.jpg').write_bytes(jpeg(packets))
    print(len(cases()))
