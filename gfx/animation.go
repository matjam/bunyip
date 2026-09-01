package gfx

// Animation is a sequence of sheet frames at a frame rate.
type Animation struct {
	Frames []int
	FPS    float32 // zero means 10
	Loop   bool
}

// AnimState plays an Animation over time.
type AnimState struct {
	Anim *Animation
	Time float64
	Done bool
}

// Play restarts the state on an animation.
func (s *AnimState) Play(a *Animation) {
	s.Anim, s.Time, s.Done = a, 0, false
}

// Advance moves time forward by dt seconds.
func (s *AnimState) Advance(dt float64) {
	if s.Anim == nil || s.Done {
		return
	}
	s.Time += dt
	if !s.Anim.Loop && s.Time >= s.length() {
		s.Time = s.length()
		s.Done = true
	}
}

func (s *AnimState) length() float64 {
	fps := s.Anim.FPS
	if fps <= 0 {
		fps = 10
	}
	return float64(len(s.Anim.Frames)) / float64(fps)
}

// Frame returns the sheet frame to draw now.
func (s *AnimState) Frame() int {
	if s.Anim == nil || len(s.Anim.Frames) == 0 {
		return 0
	}
	fps := s.Anim.FPS
	if fps <= 0 {
		fps = 10
	}
	i := int(s.Time * float64(fps))
	if s.Anim.Loop {
		i %= len(s.Anim.Frames)
	} else {
		i = min(i, len(s.Anim.Frames)-1)
	}
	return s.Anim.Frames[i]
}
