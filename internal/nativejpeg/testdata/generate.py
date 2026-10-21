from PIL import Image
from pathlib import Path
root=Path(__file__).resolve().parent;root.mkdir(parents=True,exist_ok=True)
for w,h in [(64,32),(17,13)]:
 im=Image.new('RGB',(w,h));im.putdata([((x*13+y*31)%256,(x*20+y*17)%256,(x*27+y*3)%256) for y in range(h) for x in range(w)])
 for q in ([30,75,95] if w==64 else [75]):
  for sub in [0,1,2]:
   for progressive in [False,True]:
    im.save(root/f'rgb-{w}x{h}-q{q}-s{sub}-p{int(progressive)}.jpg',quality=q,subsampling=sub,progressive=progressive)
 gray=Image.new('L',(w,h));gray.putdata([(x*19+y*43)%256 for y in range(h) for x in range(w)])
 for progressive in [False,True]:gray.save(root/f'gray-{w}x{h}-p{int(progressive)}.jpg',quality=75,progressive=progressive)
im=Image.new('RGB',(64,32));im.putdata([((x*13+y*31)%256,(x*20+y*17)%256,(x*27+y*3)%256) for y in range(32) for x in range(64)])
for progressive in [False,True]:
 for unit in ['blocks','rows']:
  im.save(root/f'rgb-64x32-q75-s2-p{int(progressive)}-restart-{unit}.jpg',quality=75,subsampling=2,progressive=progressive,**{f'restart_marker_{unit}':1})
print('Generated synthetic JPEG fixtures:',len(list(root.glob('*.jpg'))))
