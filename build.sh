for i in allocator proxy
do
    cd ./$i
    go build
    cd ..
done
