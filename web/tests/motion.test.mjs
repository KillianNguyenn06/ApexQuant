import test from 'node:test';
import assert from 'node:assert/strict';
import { CameraMotion, motionDuration } from '../assets/motion.mjs';

const first = {first:0,last:1,min:90,max:110}, next = {first:0,last:2,min:80,max:120};
function clock() {
  let time=0,id=0; const frames=new Map(), cancelled=[];
  const motion=new CameraMotion({now:()=>time,request:callback=>{frames.set(++id,callback);return id;},cancel:id=>{cancelled.push(frames.get(id));frames.delete(id);}});
  return {motion,frames,cancelled,tick:value=>{time=value;const callbacks=[...frames.values()];frames.clear();callbacks.forEach(callback=>callback(time));}};
}
test('camera moves between axis ranges and settles exactly without changing input',()=>{
  const {motion,frames,tick}=clock(), seen=[], before=JSON.stringify([first,next]);
  motion.move(first,camera=>seen.push({...camera}),600); assert.equal(frames.size,0);
  motion.move(next,camera=>seen.push({...camera}),600); assert.equal(frames.size,1);
  tick(300); assert.deepEqual(seen.at(-1),{first:0,last:1.5,min:85,max:115}); assert.equal(frames.size,1);
  tick(600); assert.deepEqual(seen.at(-1),next); assert.equal(frames.size,0);
  assert.equal(JSON.stringify([first,next]),before);
});
test('pause/reduced motion snaps and cancelled frames cannot repaint',()=>{
  const {motion,frames,cancelled}=clock(), seen=[];
  motion.move(first,camera=>seen.push({...camera})); motion.move(next,camera=>seen.push({...camera}),600);
  motion.move(first,camera=>seen.push({...camera}),0);
  const length=seen.length; cancelled[0](300);
  assert.equal(seen.length,length); assert.equal(frames.size,0); assert.deepEqual(motion.camera,first);
});
test('a new target starts from current camera with only one pending frame',()=>{
  const {motion,frames,tick}=clock(); motion.move(first,()=>{}); motion.move(next,()=>{},600); tick(300);
  const midway={...motion.camera}, newer={first:1,last:3,min:70,max:130};
  motion.move(newer,()=>{},80); assert.deepEqual(motion.camera,midway); assert.equal(frames.size,1);
  tick(380); assert.deepEqual(motion.camera,newer); assert.equal(frames.size,0);
  motion.clear(); assert.equal(motion.camera,null); assert.equal(motion.target,null);
});
test('transition duration fits playback intervals without changing their speed',()=>{
  assert.equal(motionDuration(1),600); assert.equal(motionDuration(.25),600);
  assert.equal(motionDuration(5),200); assert.equal(motionDuration(10),100); assert.equal(motionDuration(20),50);
  for(const speed of [.25,.5,1,2,5,10,20]) assert.ok(motionDuration(speed)<=1000/speed);
});

test('5, 10 and 20 days/sec pan steadily throughout consecutive day intervals',()=>{
  for (const speed of [5,10,20]) {
    const {motion,frames,tick}=clock(), duration=motionDuration(speed);
    motion.move(first,()=>{});
    motion.move(next,()=>{},duration);
    tick(duration/4);
    assert.equal(motion.camera.last,1.25); // Linear pan, no per-day acceleration.
    tick(duration/2); assert.equal(motion.camera.last,1.5);
    tick(duration); assert.deepEqual(motion.camera,next); assert.equal(frames.size,0);
    const later={...next,first:1,last:3};
    motion.move(later,()=>{},duration); tick(duration*1.5);
    assert.equal(motion.camera.last,2.5); assert.equal(frames.size,1);
    motion.move(later,()=>{},0); assert.equal(frames.size,0); assert.deepEqual(motion.camera,later);
  }
});
