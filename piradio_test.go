package main

import (
	"testing"
)

func TestBeautify(t *testing.T) {
	camelCasePtr = new(bool)
	*camelCasePtr = false

	s := beautify("abc")
	if s != "abc" {
		t.Error("abc", s)
	}

	s = beautify("äöüß")
	if s != "aeoeuess" {
		t.Error("aeoeuess", s)
	}

	s = beautify("abcdefghijklmnopqrstuvwxyz")
	if s != "abcdefghijklmnopqrstuvwxyz" {
		t.Error("abcdefghijklmnopqrstuvwxyz", s)
	}

	*camelCasePtr = true
	s = beautify("abcdefghijklmnopqrstuvwxyz")
	if s != "abcdefghijklmnopqrstuvwxyz" {
		t.Error("abcdefghijklmnopqrstuvwxyz", s)
	}

	s = beautify("abc def ghi jkl MNO pqrst uvwxyz")
	if s != "Abc Def Ghi Jkl Mno Pqrst Uvwxyz" {
		t.Error("Abc Def Ghi Jkl Mno Pqrst Uvwxyz", s)
	}

	s = beautify("abc MNO uvwâxyz")
	if s != "Abc Mno Uvwxyz" {
		t.Error("Abc Mno Uvwxyz", s)
	}
}

func TestIsAllLowercase(t *testing.T) {
	b := isOnlyLowerCase("abc/%&!# _,;:()[]{}")
	if !b {
		t.Error("abc", b)
	}

	b = isOnlyLowerCase("aBc/%&!# _,;:()[]{}")
	if b {
		t.Error("aBc", b)
	}
}

func setDebug() {
	debug = new(bool)
	*debug = false
}

func TestRemoveCdmEditNoise(t *testing.T) {
	setDebug()
	t1 := "Memory Pages (CDM Radio Edit)"
	res := removeNoise(t1)
	if res != "Memory Pages" {
		t.Error("TestRemoveCdmEditNoise :", res)
	}
}

func TestRemoveTrailingNoise(t *testing.T) {
	setDebug()
	t1 := "Memory Pages (CDM Radio Edit) and more"
	res := removeNoise(t1)
	if res != "Memory Pages and more" {
		t.Error("TestRemoveTrailingNoise :", res)
	}
}

func TestRemoveMixNoise(t *testing.T) {
	setDebug()
	t3 := "DANCING SEAHORSES FEAT. MARC HARTMAN (ORIGINAL MIX)"
	res := removeNoise(t3)
	if res != "DANCING SEAHORSES FEAT. MARC HARTMAN" {
		t.Error("TestRemoveMixNoise :", res)
	}
}

func TestRemoveCutNoise(t *testing.T) {
	setDebug()
	t3 := "Under The Radar (Dragonflight Cut)"
	res := removeNoise(t3)
	if res != "Under The Radar" {
		t.Error("TestRemoveCutNoise :", res)
	}
}

func TestRemoveNoNoise(t *testing.T) {
	setDebug()
	t4 := "After dark (On The Road Again)"
	res := removeNoise(t4)
	if res != "After dark (On The Road Again)" {
		t.Error("TestRemoveNoNoise :", res)
	}
}

func TestRemoveRemixNoise(t *testing.T) {
	setDebug()
	t4 := "Tide (Electro RMX)"
	res := removeNoise(t4)
	if res != "Tide" {
		t.Error("TestRemoveRemixNoise :", res)
	}
}

func TestRemoveCoverNoise(t *testing.T) {
	setDebug()
	t4 := "Slave to the rhythm .(cover)"
	res := removeNoise(t4)
	if res != "Slave to the rhythm" {
		t.Error("TestRemoveCoverNoise :", res)
	}
}
