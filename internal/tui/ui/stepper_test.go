package ui

import "testing"

func TestStepperBoundsAndKeys(t *testing.T) {
	s := NewStepper("t/max", 1, 99, 1)
	s.SetValue(Value{Num: 2, Set: true})
	if got := render(s.View(true, 40)); got != "› [ − ] 2 [ + ]" {
		t.Fatalf("view %q", got)
	}
	for _, k := range []string{"+", "=", "right"} {
		s.Update(key(k))
	}
	if s.Value().Num != 5 {
		t.Fatalf("up keys: %d", s.Value().Num)
	}
	for i := 0; i < 10; i++ {
		s.Update(key("-"))
	}
	if s.Value().Num != 1 {
		t.Fatalf("lower bound: %d", s.Value().Num)
	}
	s.Update(key("left"))
	if s.Value().Num != 1 {
		t.Fatalf("left below min: %d", s.Value().Num)
	}
}

func TestStepperTypedDigits(t *testing.T) {
	s := NewStepper("t/max", 1, 99, 1)
	typeText(s, "12")
	if s.Value().Num != 12 {
		t.Fatalf("typed 12: %d", s.Value().Num)
	}
	typeText(s, "3") // 123 > 99 starts over
	if s.Value().Num != 3 {
		t.Fatalf("typed past max: %d", s.Value().Num)
	}
	typeText(s, "4")
	s.Update(key("backspace"))
	if s.Value().Num != 3 {
		t.Fatalf("backspace: %d", s.Value().Num)
	}
	s.Update(key("backspace"))
	if s.Value().Num != 1 {
		t.Fatalf("backspace to empty clamps to min: %d", s.Value().Num)
	}
	s.Blur()
	typeText(s, "7")
	if s.Value().Num != 7 {
		t.Fatalf("typing after blur starts fresh: %d", s.Value().Num)
	}
	if !s.TakesKey(key("4")) || s.TakesKey(key("q")) || s.TakesKey(key("down")) {
		t.Fatal("TakesKey")
	}
}

func TestStepperStepAndSuffix(t *testing.T) {
	s := NewStepper("t/disk", 1, 100, 5)
	s.Suffix = "%"
	s.SetValue(Value{Num: 80, Set: true})
	s.Update(key("+"))
	if render(s.View(false, 40)) != "  [ − ] 85% [ + ]" {
		t.Fatalf("view %q", render(s.View(false, 40)))
	}
	for i := 0; i < 5; i++ {
		s.Update(key("+"))
	}
	if s.Value().Num != 100 {
		t.Fatalf("upper bound: %d", s.Value().Num)
	}
	typeText(s, "83")
	if s.Value().Num != 83 {
		t.Fatalf("typed values need not be multiples of the step: %d", s.Value().Num)
	}
}

func TestStepperDefaultUntilTouched(t *testing.T) {
	s := NewStepper("t/repo-max", 0, 99, 1)
	s.ZeroText = "∞"
	s.SetValue(Value{})
	s.Default = 1
	if got := render(s.View(false, 40)); got != "  [ − ] 1 [ + ] (default)" {
		t.Fatalf("queue default %q", got)
	}
	s.Default, s.DefaultText = 0, "∞"
	if got := render(s.View(false, 40)); got != "  [ − ] ∞ [ + ] (default)" {
		t.Fatalf("all default %q", got)
	}
	s.Update(key("+"))
	if v := s.Value(); !v.Set || v.Num != 1 {
		t.Fatalf("first + from the default: %+v", v)
	}
	s.Update(key("-"))
	if got := render(s.View(false, 40)); got != "  [ − ] ∞ [ + ]" {
		t.Fatalf("explicit zero %q", got)
	}
}

func TestStepperMouse(t *testing.T) {
	s := NewStepper("t/max", 1, 99, 1)
	s.SetValue(Value{Num: 2, Set: true})
	s.Update(clickAt(t, s.View(false, 40), "t/max/inc"))
	s.Update(clickAt(t, s.View(false, 40), "t/max/inc"))
	s.Update(clickAt(t, s.View(false, 40), "t/max/dec"))
	if s.Value().Num != 3 {
		t.Fatalf("clicks: %d", s.Value().Num)
	}
	if !s.Hit(clickAt(t, s.View(false, 40), "t/max")) || s.Hit(outside) {
		t.Fatal("Hit")
	}
}
