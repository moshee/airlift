#!/bin/bash

styles=`chroma --list | grep 'styles:' | cut -d ":" -f2`
for style in $styles
do
  `chroma --style="$style" --html-styles > static/syntax/$style.css`
done
