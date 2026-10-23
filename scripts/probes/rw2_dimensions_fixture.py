"""Original metadata-only RW2 fixtures; Go tests use the frozen input bytes."""
import base64, json, sys
from pathlib import Path
from raw_dimensions_fixture import encode

# Filled with an original solid-color synthetic JPEG. No camera image is used.
JPEG_BASE64 = "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wAARCAAgAEADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwDzeiiivWPPCiiigAooooAKKKKACiiigAooooAKKKKACiiigD//2Q=="

def fields(left=10,right=4010,top=20,bottom=3020,kind=4):
    return {2:(kind,8016),3:(kind,6016),4:(kind,top),5:(kind,left),6:(kind,bottom),7:(kind,right)}

def cases():
    normal=fields();jpeg=base64.b64decode(JPEG_BASE64)
    larger=bytearray(jpeg);sof=larger.index(b'\xff\xc0')
    larger[sof+5:sof+7]=(240).to_bytes(2,'big');larger[sof+7:sof+9]=(320).to_bytes(2,'big')
    definitions=[
        ('normal',[{'tags':normal}]),
        ('sensor-only',[{'tags':{2:(4,8016),3:(4,6016)}}]),
        ('borders-only',[{'tags':{k:v for k,v in normal.items() if k>=4}}]),
        ('zero',[{'tags':fields(left=0,right=0,top=0,bottom=0)}]),
        ('negative-width',[{'tags':fields(left=4010,right=10)}]),
        ('negative-height',[{'tags':fields(top=3020,bottom=20)}]),
        ('wide',[{'tags':fields(left=10,right=70010,top=20,bottom=80020)}]),
        ('int32-max',[{'tags':fields(left=0,right=2147483647)}]),
        ('int32-min',[{'tags':fields(left=2147483648,right=0)}]),
        ('positive-overflow',[{'tags':fields(left=0,right=4294967295)}]),
        ('negative-overflow',[{'tags':fields(left=4294967295,right=0)}]),
        ('short',[{'tags':fields(kind=3)}]),
        ('ordinary-conflict',[{'tags':normal,'width':64,'height':32}]),
        ('next-borders',[{'next':1},{'tags':normal}]),
        ('sub-borders',[{'sub':1},{'tags':normal}]),
        ('exif-borders',[{'exif':1},{'tags':normal}]),
        ('unsorted',[{'tags':normal,'unsorted':True}]),
        ('crop-conflict',[{'tags':normal|{47:(4,100),48:(4,200),49:(4,500),50:(4,700)}}]),
        ('jpeg-only',[{'jpeg':jpeg}]),
        ('jpeg-borders',[{'tags':normal,'jpeg':jpeg}]),
        ('jpeg-width-only',[{'tags':{5:normal[5],7:normal[7]},'jpeg':jpeg}]),
        ('jpeg-height-only',[{'tags':{4:normal[4],6:normal[6]},'jpeg':jpeg}]),
        ('jpeg-zero',[{'tags':fields(left=0,right=0,top=0,bottom=0),'jpeg':jpeg}]),
        ('jpeg-invalid',[{'jpeg':b'not a JPEG fixture'}]),
        ('next-ordinary',[{'next':1},{'width':320,'height':240}]),
        ('sub-ordinary',[{'sub':1},{'width':320,'height':240}]),
        ('exif-ordinary',[{'exif':1},{'width':320,'height':240}]),
        ('jpeg-next',[{'jpeg':jpeg,'next':1},{'width':320,'height':240}]),
        ('jpeg-next-jpeg',[{'jpeg':jpeg,'next':1},{'jpeg':bytes(larger)}]),
        ('jpeg-exif',[{'jpeg':jpeg,'exif':1},{'width':320,'height':240}]),
        ('exif-jpeg-unsorted',[{'jpeg':jpeg,'exif':1,'unsorted':True},{'width':320,'height':240}]),
        ('jpeg-main-exif',[{'jpeg':jpeg,'exif':1},{'width':320,'height':240,'subfile':0}]),
        ('next-border-override',[{'tags':normal,'next':1},{'tags':fields(right=2010,bottom=1520)}]),
        ('width-exif-height',[{'tags':{5:normal[5],7:normal[7]},'exif':1},{'width':320,'height':240}]),
        ('orientation',[{'tags':normal|{274:(3,6)}}]),
    ]
    for missing in [4,5,6,7]:
        definitions.append(('missing-'+str(missing),[{'tags':{k:v for k,v in normal.items() if k!=missing}}]))
    for endian in ['<','>']:
        for name,directories in definitions:
            data=encode(directories,endian,magic=85,rw2=True)
            yield {'name':('le-' if endian=='<' else 'be-')+name+'.rw2','data':base64.b64encode(data).decode()}
        for suffix,rw2 in [('raw',False),('rwl',True)]:
            yield {'name':('le-' if endian=='<' else 'be-')+'family.'+suffix,'data':base64.b64encode(encode([{'tags':normal}],endian,magic=85,rw2=rw2)).decode()}
        yield {'name':('le-' if endian=='<' else 'be-')+'tiff-container.rw2','data':base64.b64encode(encode([{'width':640,'height':480,'tags':normal}],endian)).decode()}

if __name__=='__main__':
    output=Path(sys.argv[1]);output.mkdir(parents=True,exist_ok=False)
    rows=list(cases())
    for row in rows:(output/row['name']).write_bytes(base64.b64decode(row['data']))
    (output/'inputs.json').write_text(json.dumps(rows,indent=1)+'\n',encoding='utf-8')
    print(len(rows))
