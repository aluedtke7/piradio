## Development
#### Cross compilation
On your development machine run the following line to build a `piradio` binary that can run on a
raspberry pi:

    GOOS=linux GOARCH=arm go build -o piradio -v piradio.go
    
This will create a binary named `piradio` that can run on an ARM processor running linux.

#### Copy binary to raspberry
If your ssh key is present on the raspberry, you don't need credentials to copy the binary:

    scp piradio pi@10.7.7.43:

or both commands in one go:

    GOOS=linux GOARCH=arm go build -o piradio -v piradio.go && scp piradio pi@10.7.7.43:

#### Run tests
Normally you're developing on a pc. Therefore, the tests will also run on this pc and you have to start
the tests as follows:

    go test piradio.go piradio_test.go -v

Please keep in mind, that you can't test the special raspberry functionalities (GPIO, I²C etc.). 
This can only be tested, if you run them on a raspberry pi with installed GO. See next chapter.

The current tests don't need a raspberry pi and will run on the dev machine without problems.

#### Update all Modules
Run the following command to update all libraries/modules:

    go get -u

#### Install GO on the raspberry
If you don't like cross compilation or if you want to run tests that needs a raspberry pi, you can install
GO directly on the raspberry. Get the golang tar archive from Google:

    curl -L https://dl.google.com/go/go1.13.linux-armv6l.tar.gz -o go1.13.linux-armv6l.tar.gz

Extract the archive:

    sudo tar -C /usr/local -xvf go1.13.linux-armv6l.tar.gz

Test the golang installation:

    go version

## Some thoughts

#### Why a command channel for the display?
The LCD and the OLED package each have a channel that must be used to send data and commands via i²c to the 
display. If we wouldn't use this approach, the commands and data would get mixed up when more than one line 
should display scrolling text. This could lead to a display that doesn't react anymore.

#### Why don't you use WaitForEdge() on the GPIO pins?
This was indeed my first attempt. With that implementation I needed a minimum debounce time of 250ms in order to work 
halfway. Even with this high debounce time I had several double button clicks when I switched through the
stations.

The final implementation works as desired and has a low debounce time of 100ms.

#### Missing artist and/or title
Most probably this behaviour is caused by the radio station. If you are in doubt, please switch the debug mode (-debug) 
on and see for yourself. The output of the mplayer is printed to stdout. The artist/title
information is found in the ICY Info StreamTitle field.
Usually, this field contains the artist and title separated by a hyphen.

Sometimes the StreamTitle field is transported but it's empty.

The other displayed parts (station name and bitrate) might also not be available.
The station name e.g. is transported in the Name field of the stream.

